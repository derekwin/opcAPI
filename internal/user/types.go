package user

import "time"

type Channel string

const (
	ChannelPhone  Channel = "phone"
	ChannelWeChat Channel = "wechat"
	ChannelFeishu Channel = "feishu"
)

type User struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	Phone        string    `json:"phone,omitempty"`
	WeChatOpenID string    `json:"wechat_openid,omitempty"`
	FeishuOpenID string    `json:"feishu_open_id,omitempty"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
}

type Tenant struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	PlanID    string    `json:"plan_id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type Membership struct {
	TenantID string    `json:"tenant_id"`
	UserID   string    `json:"user_id"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}

type TenantUser struct {
	User User   `json:"user"`
	Role string `json:"role"`
}

type SessionClaims struct {
	UserID   string `json:"user_id"`
	TenantID string `json:"tenant_id"`
	Role     string `json:"role"`
	Exp      int64  `json:"exp"`
}

type SignupRequest struct {
	Channel          string `json:"channel"`
	Identifier       string `json:"identifier"`
	VerificationCode string `json:"verification_code,omitempty"`
	TenantName       string `json:"tenant_name,omitempty"`
}

type SigninRequest struct {
	Channel          string `json:"channel"`
	Identifier       string `json:"identifier"`
	VerificationCode string `json:"verification_code,omitempty"`
}

type CreateTenantUserRequest struct {
	Channel          string `json:"channel"`
	Identifier       string `json:"identifier"`
	VerificationCode string `json:"verification_code,omitempty"`
	Role             string `json:"role"`
}

type AuthResult struct {
	Token  string        `json:"token"`
	User   User          `json:"user"`
	Tenant Tenant        `json:"tenant"`
	Role   string        `json:"role"`
	Claims SessionClaims `json:"claims"`
}
