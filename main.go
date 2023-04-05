package main

import (
	"aseel/pointSystem/pointSystemPb"
	"context"
	"fmt"
	"github.com/golang-jwt/jwt"
	"github.com/joho/godotenv"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

type ActivityType int

type server struct {
	db *gorm.DB
}

type User struct {
	Id        int       `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"name" json:"name"`
	Email     string    `gorm:"email" json:"email"`
	Password  string    `gorm:"password" json:"password"`
	Role      string    `gorm:"role" json:"role"`
	Points    float64   `gorm:"points"  json:"points"`
	CreatedAt time.Time `gorm:"created_at" json:"created_at"`
	UpdatedAt time.Time `gorm:"updated_at" json:"updated_at"`
}

type ActivityHistory struct {
	Id        int          `gorm:"primaryKey" json:"id"`
	UserId    int          `gorm:"user_id, foreignKey:Users " json:"user_id"`
	Activity  ActivityType `gorm:"activity_type" json:"activity_type"`
	Points    float64      `gorm:"points" json:"points"`
	CreatedAt time.Time    `gorm:"created_at" json:"created_at"`
}

func hashPassword(password string) string {
	bytes, _ := bcrypt.GenerateFromPassword([]byte(password), 5)

	return string(bytes)
}

func generateToken(user User) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"email": user.Email,
		"role":  user.Role,
		"exp":   time.Now().Add(time.Hour * 24).Unix(),
	})

	tokenString, err := token.SignedString([]byte("secret"))
	if err != nil {
		fmt.Errorf("something went wrong: %s", err.Error())
		return "", err
	}

	return tokenString, nil
}

func (s server) SignUp(ctx context.Context, user *pointSystemPb.User) (*pointSystemPb.UserResponse, error) {
	var newUser User

	if err := s.db.Where(&User{Email: user.Email}).First(&newUser).Error; err != nil {
		return &pointSystemPb.UserResponse{
			Status: http.StatusConflict,
			Error:  "user already exists",
		}, err
	}
	newUser.Email = user.Email
	newUser.Name = user.Name
	newUser.Role = user.Role
	newUser.Password = hashPassword(user.Password)

	s.db.Create(&newUser)

	return &pointSystemPb.UserResponse{
		Status: http.StatusCreated,
		Error:  "",
	}, nil
}

func (s server) SignIn(ctx context.Context, request *pointSystemPb.SignInRequest) (*pointSystemPb.SignInResponse, error) {
	var user User
	if err := s.db.Where(&User{Email: request.Email}).First(&user).Error; err != nil {
		return &pointSystemPb.SignInResponse{
			Status: http.StatusNotFound,
			Error:  "user not found",
		}, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(request.Password)); err != nil {
		return &pointSystemPb.SignInResponse{
			Status: http.StatusUnauthorized,
			Error:  "wrong password",
		}, err
	}

	token, err := generateToken(user)
	if err != nil {
		return &pointSystemPb.SignInResponse{
			Status: http.StatusInternalServerError,
			Error:  "internal server error",
		}, err
	}

	return &pointSystemPb.SignInResponse{
		Status:      http.StatusOK,
		Error:       "",
		AccessToken: token,
	}, nil
}

func (s server) GetPoints(ctx context.Context, request *pointSystemPb.GetPointsRequest) (*pointSystemPb.GetPointsResponse, error) {

	var user User
	if err := s.db.Where(&User{Email: request.Email}).First(&user).Error; err != nil {
		return &pointSystemPb.GetPointsResponse{
			Status: http.StatusNotFound,
			Error:  "user not found",
		}, err
	}

	return &pointSystemPb.GetPointsResponse{
		Status: http.StatusOK,
		Error:  "",
		Points: float32(user.Points),
	}, nil

}

func (s server) AddActivity(ctx context.Context, request *pointSystemPb.AddActivityRequest) (*pointSystemPb.AddActivityResponse, error) {

	var user User
	if err := s.db.Where(&User{Email: request.Email}).First(&user).Error; err != nil {
		return &pointSystemPb.AddActivityResponse{
			Message: "user not found",
		}, err
	}

	if user.Role != "admin" {
		return &pointSystemPb.AddActivityResponse{
			Message: "user is not admin",
		}, nil
	}

	var activity ActivityHistory

	activity.UserId = int(request.UserId)
	activity.Activity = ActivityType(request.ActivityType)
	activity.Points = float64(request.Points)
	activity.CreatedAt = time.Now()

	s.db.Create(&activity)

	return &pointSystemPb.AddActivityResponse{Message: fmt.Sprintf("Activity name %s has been added", request.ActivityType)}, nil
}

func (s server) SendPoints(ctx context.Context, request *pointSystemPb.SendPointsRequest) (*pointSystemPb.SendPointsResponse, error) {
	var sender User
	var receiver User

	if err := s.db.Where(&User{Email: request.SenderEmail}).First(&sender).Error; err != nil {
		return &pointSystemPb.SendPointsResponse{
			Status: http.StatusNotFound,
			Error:  "sender not found",
		}, err
	}

	if err := s.db.Where(&User{Email: request.ReceiverEmail}).First(&receiver).Error; err != nil {
		return &pointSystemPb.SendPointsResponse{
			Status: http.StatusNotFound,
			Error:  "receiver not found",
		}, err
	}

	if request.SenderEmail == request.ReceiverEmail {
		return &pointSystemPb.SendPointsResponse{
			Status: http.StatusConflict,
			Error:  "sender and receiver are the same user",
		}, nil
	}

	if sender.Points < float64(request.Points) {
		return &pointSystemPb.SendPointsResponse{
			Status: http.StatusConflict,
			Error:  "sender doesn't have enough points",
		}, nil
	}

	sender.Points -= float64(request.Points)
	receiver.Points += float64(request.Points)

	s.db.Save(&sender)
	s.db.Save(&receiver)

	return &pointSystemPb.SendPointsResponse{
		Status:  http.StatusOK,
		Error:   "",
		Message: "points sent successfully",
	}, nil
}

func (s server) SpendPoints(ctx context.Context, request *pointSystemPb.SpendPointsRequest) (*pointSystemPb.SpendPointsResponse, error) {
	var user User
	if err := s.db.Where(&User{Email: request.Email}).First(&user).Error; err != nil {
		return &pointSystemPb.SpendPointsResponse{
			Status: http.StatusNotFound,
			Error:  "user not found",
		}, err
	}

	if user.Points < float64(request.Points) {
		return &pointSystemPb.SpendPointsResponse{
			Status: http.StatusConflict,
			Error:  "user doesn't have enough points",
		}, nil
	}

	user.Points -= float64(request.Points)
	s.db.Save(&user)

	return &pointSystemPb.SpendPointsResponse{
		Status:  http.StatusOK,
		Error:   "",
		Message: "points spent successfully",
	}, nil
}

func connectToDB(db *gorm.DB) (*gorm.DB, error) {
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading .env file")
	}
	dbName := os.Getenv("DB_NAME")
	dbUser := os.Getenv("DB_USER")
	dbPassword := os.Getenv("DB_PASSWORD")
	dbHost := os.Getenv("DB_HOST")
	dbPort := os.Getenv("DB_PORT")
	dsn := fmt.Sprintf("%s?charset=utf8mb4&parseTime=True&loc=Local", dbUser+":"+dbPassword+"@tcp("+dbHost+":"+dbPort+")/"+dbName)
	fmt.Printf("dsn: %s", dsn)
	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect database: %v", err)
		return nil, err
	}
	db.AutoMigrate(&User{}, &ActivityHistory{})

	return db, nil
}

func main() {
	s := server{}
	_, err := connectToDB(s.db)
	if err != nil {
		log.Fatalf("failed to connect database: %v", err)
		return
	}
	fmt.Println("connected to db successfully")

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", os.Getenv("PORT")))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	srv := grpc.NewServer()
	pointSystemPb.RegisterPointSystemServer(srv, &server{})
	reflection.Register(srv)
	if err = srv.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
