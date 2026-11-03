package passwordreset

type CodeRequest struct {
	Email string `json:"email" validate:"required,max=254"`
}

type ResetRequest struct {
	Email       string `json:"email" validate:"required,max=254"`
	Code        string `json:"code" validate:"required,len=6,numeric"`
	NewPassword string `json:"new_password" validate:"required,min=8,max=72"`
}
