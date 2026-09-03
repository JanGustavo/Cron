package handler

import (
	"testing"
	"time"

	"github.com/JanGustavo/Cron/internal/auth"
	userDomain "github.com/JanGustavo/Cron/internal/domain/user"
)

func TestIsValidCPF(t *testing.T) {
	if !isValidCPF("11144477735") {
		t.Fatal("expected valid CPF")
	}

	if isValidCPF("11144477736") {
		t.Fatal("expected invalid CPF")
	}
}

func TestIsValidCNPJ(t *testing.T) {
	if !isValidCNPJ("11222333000181") {
		t.Fatal("expected valid CNPJ")
	}

	if isValidCNPJ("11222333000182") {
		t.Fatal("expected invalid CNPJ")
	}
}

func TestIsValidTaxDocument(t *testing.T) {
	if !isValidTaxDocument("11144477735", "cpf") {
		t.Fatal("expected valid CPF document")
	}

	if !isValidTaxDocument("11222333000181", "cnpj") {
		t.Fatal("expected valid CNPJ document")
	}

	if isValidTaxDocument("123", "cpf") {
		t.Fatal("expected invalid short document")
	}
}

func TestPasswordHashingAndVerification(t *testing.T) {
	plainPassword := "SegredoForte@2026"

	hash, err := auth.HashPassword(plainPassword)
	if err != nil {
		t.Fatalf("unexpected error hashing password: %v", err)
	}

	if hash == "" || hash == plainPassword {
		t.Fatal("expected non-empty encrypted hash different from plain password")
	}

	if !auth.CheckPasswordHash(plainPassword, hash) {
		t.Fatal("expected CheckPasswordHash to return true for correct password")
	}

	if auth.CheckPasswordHash("SenhaIncorreta123", hash) {
		t.Fatal("expected CheckPasswordHash to return false for incorrect password")
	}
}

func TestBuildUserCSVRowIncludesRealJobCountAndEscapesValues(t *testing.T) {
	createdAt := time.Date(2025, 1, 17, 12, 0, 0, 0, time.UTC)
	user := &userDomain.User{
		ID:         "abc123",
		Email:      "joao,teste@example.com",
		Plan:       userDomain.PlanPaid,
		FullName:   "João \"Teste\"",
		Role:       "admin",
		IsVerified: true,
		CreatedAt:  createdAt,
	}

	row := buildUserCSVRow(user, 42, "monthly", "2025-02-15", "active", "asaas", "cust-1", "sub-9")
	want := "abc123,\"joao,teste@example.com\",\"João \"\"Teste\"\"\",pro,admin,true,42,monthly,2025-02-15,active,asaas,cust-1,sub-9,2025-01-17\n"

	if got := row; got != want {
		t.Fatalf("unexpected CSV row\nwant: %q\n got: %q", want, got)
	}
}
