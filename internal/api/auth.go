package api

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/zhaocore/agentdock/internal/store/mysql"
	"golang.org/x/crypto/bcrypt"
)

// @Summary 用户注册
// @Tags Auth
// @Accept json
// @Produce json
// @Param body body RegisterRequest true "注册信息"
// @Success 201 {object} AuthResponse
// @Router /v1/auth/register [post]
func (h *Handler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// bcrypt 哈希密码
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("bcrypt hash: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process registration"})
		return
	}

	account, err := mysql.RegisterAccount(c.Request.Context(), h.DB, req.Email, string(hash))
	userID := account.ID
	if err != nil {
		if errors.Is(err, mysql.ErrRegistrationClosed) {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		// 检查唯一约束冲突
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "email already registered"})
			return
		}
		log.Printf("insert user: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
		return
	}

	token, err := h.generateJWT(userID, req.Email, account.AuthVersion)
	if err != nil {
		log.Printf("generate jwt: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	c.JSON(http.StatusCreated, AuthResponse{
		Token: token,
		Email: req.Email,
	})
}

// @Summary 用户登录
// @Tags Auth
// @Accept json
// @Produce json
// @Param body body LoginRequest true "登录信息"
// @Success 200 {object} AuthResponse
// @Router /v1/auth/login [post]
func (h *Handler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	account, passwordHash, err := mysql.FindAccount(c.Request.Context(), h.DB, req.Email)
	userID, email := account.ID, account.Email
	if err == sql.ErrNoRows {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	}
	if err != nil {
		log.Printf("query user: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "login failed"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	}

	if account.Status != "active" {
		c.JSON(http.StatusForbidden, gin.H{"error": "account is disabled or being deleted"})
		return
	}
	token, err := h.generateJWT(userID, email, account.AuthVersion)
	if err != nil {
		log.Printf("generate jwt: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, AuthResponse{
		Token: token,
		Email: email,
	})
}

// @Summary 修改密码
// @Tags Auth
// @Accept json
// @Produce json
// @Param body body ChangePasswordRequest true "密码修改"
// @Success 200 {object} map[string]string
// @Router /v1/auth/change-password [post]
func (h *Handler) ChangePassword(c *gin.Context) {
	// API 令牌不得修改账号密码（提权链）
	if denyAPIToken(c) {
		return
	}
	userID := c.GetString("user_id")

	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.NewPassword == req.OldPassword {
		c.JSON(400, gin.H{"error": "new password must differ from temporary or current password"})
		return
	}
	var currentHash string
	err := h.DB.QueryRowContext(c.Request.Context(),
		`SELECT password_hash FROM users WHERE id = ?`,
		userID,
	).Scan(&currentHash)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "current password is incorrect"})
		return
	}
	if err != nil {
		log.Printf("query user: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to verify password"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(req.OldPassword)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "current password is incorrect"})
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("bcrypt hash: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process password change"})
		return
	}

	err = mysql.ChangeAccountPassword(c.Request.Context(), h.DB, userID, currentHash, string(newHash))
	if err != nil {
		log.Printf("update password: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update password"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "password changed"})
}

// @Summary 当前用户信息
// @Tags Auth
// @Produce json
// @Success 200 {object} UserResponse
// @Router /v1/auth/me [get]
func (h *Handler) CurrentUser(c *gin.Context) {
	// API 令牌不得读取账号信息（提权链）
	if denyAPIToken(c) {
		return
	}
	userID := c.GetString("user_id")

	resp, err := mysql.GetAccount(c.Request.Context(), h.DB, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// generateJWT 签发 JWT token。
func (h *Handler) generateJWT(userID, email string, version int64) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub":   userID,
		"ver":   version,
		"email": email,
		"iss":   "graphgo",
		"iat":   now.Unix(),
		"exp":   now.Add(h.Config.Auth.TokenExpiry).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(h.Config.Auth.JWTSecret))
}

// isUniqueViolation 检测数据库唯一约束冲突。
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Duplicate entry") || strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint")
}

// --- 请求/响应结构 ---

// RegisterRequest 注册请求。
type RegisterRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6,max=72"`
}

// LoginRequest 登录请求。
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// ChangePasswordRequest 修改密码请求。
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=6,max=72"`
}

// AuthResponse 认证响应。
type AuthResponse struct {
	Token string `json:"token"`
	Email string `json:"email"`
}

// UserResponse 用户信息响应。
type UserResponse struct {
	IsAdmin            bool   `json:"is_admin"`
	MustChangePassword bool   `json:"must_change_password"`
	Status             string `json:"status"`
	ID                 string `json:"id"`
	Email              string `json:"email"`
	CreatedAt          string `json:"created_at"`
}
