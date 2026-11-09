// Structs de petición y respuesta (JSON en camelCase).
package handler

import (
	"time"

	"accounts-api/internal/model"
	"accounts-api/internal/service"
)

// timeLayout es el formato de todas las fechas de la API (I-09):
// ISO 8601 en UTC con exactamente 3 decimales y sufijo Z.
const timeLayout = "2006-01-02T15:04:05.000Z"

// formatTime convierte una fecha a UTC y la formatea según I-09.
func formatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

// userResponse es la forma de un usuario (contrato, sección 1). Nunca incluye la contraseña.
type userResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	CreatedAt string `json:"createdAt"`
}

func newUserResponse(u model.User) userResponse {
	return userResponse{
		ID:        u.ID,
		Name:      u.Name,
		Email:     u.Email,
		Role:      string(u.Role),
		CreatedAt: formatTime(u.CreatedAt),
	}
}

// tokenPairResponse es la forma de un par de tokens (contrato, sección 1).
type tokenPairResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	TokenType    string `json:"tokenType"` // siempre "Bearer"
	ExpiresIn    int    `json:"expiresIn"`
}

func newTokenPairResponse(p service.TokenPair) tokenPairResponse {
	return tokenPairResponse{
		AccessToken:  p.AccessToken,
		RefreshToken: p.RefreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    p.ExpiresIn,
	}
}

// accountResponse es la forma de una cuenta (contrato, sección 1). El dueño y el sobregiro
// no se exponen.
type accountResponse struct {
	ID            string `json:"id"`
	AccountNumber string `json:"accountNumber"`
	AccountType   string `json:"accountType"`
	Balance       int64  `json:"balance"` // centavos
	CreatedAt     string `json:"createdAt"`
}

func newAccountResponse(a model.Account) accountResponse {
	return accountResponse{
		ID:            a.ID,
		AccountNumber: a.Number,
		AccountType:   string(a.Kind),
		Balance:       a.BalanceCents,
		CreatedAt:     formatTime(a.CreatedAt),
	}
}

// accountListResponse es la respuesta de GET /accounts.
type accountListResponse struct {
	Items []accountResponse `json:"items"`
}

// movementResponse es la forma de un movimiento (contrato, sección 1). amount siempre es
// positivo; el signo lo da movementType.
type movementResponse struct {
	ID           string `json:"id"`
	AccountID    string `json:"accountId"`
	MovementType string `json:"movementType"`
	Amount       int64  `json:"amount"`
	Details      any    `json:"details"`
	CreatedAt    string `json:"createdAt"`
}

// Detalles según el tipo de movimiento.
type rechargeDetails struct {
	RechargeType string `json:"rechargeType"`
}

type transferDetails struct {
	RelatedAccountID string `json:"relatedAccountId"`
}

func newMovementResponse(m model.Movement) movementResponse {
	var details any = struct{}{} // withdrawal: sin detalles
	switch m.Kind {
	case model.MovementRecharge:
		details = rechargeDetails{RechargeType: string(m.RechargeType)}
	case model.MovementTransferIn, model.MovementTransferOut:
		details = transferDetails{RelatedAccountID: m.RelatedAccountID}
	}
	return movementResponse{
		ID:           m.ID,
		AccountID:    m.AccountID,
		MovementType: string(m.Kind),
		Amount:       m.AmountCents,
		Details:      details,
		CreatedAt:    formatTime(m.CreatedAt),
	}
}
