package user

// Profile represents the authenticated system operator's identity
type Profile struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}
