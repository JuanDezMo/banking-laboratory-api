// User y Role.
package model

import "time"

// Role es el rol de un usuario (spec 4.1).
type Role string

const (
	RoleCustomer Role = "customer" // se registra solo y opera sus cuentas
	RoleOperator Role = "operator" // se crea con una carga inicial; hace recargas (RN-22, RN-27)
)

// User es un usuario registrado. PasswordHash nunca sale en una respuesta.
type User struct {
	ID           string
	Name         string
	Email        string // recortado y en minúsculas
	PasswordHash string // argon2id en formato PHC
	Role         Role
	CreatedAt    time.Time
}
