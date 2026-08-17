package handler

import "testing"

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
