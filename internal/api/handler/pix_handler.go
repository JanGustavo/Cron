package handler

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"

	"github.com/skip2/go-qrcode"
)

type PixHandler struct{}

func NewPixHandler() *PixHandler {
	return &PixHandler{}
}

const (
	PixKey  = "Jeeh2200@gmail.com"
	PixName = "Janderson Gustavo"
	PixCity = "Bayeux"
)

type SugestaoValor struct {
	Label string `json:"label"`
	Valor string `json:"valor"`
}

var ValoresSugeridos = []SugestaoValor{
	{Label: "☕ Café", Valor: "0.50"},
	{Label: "🍕 Apoio", Valor: "1.00"},
	{Label: "🚀 Top", Valor: "2.00"},
}

// ListValores — GET /v1/pix/valores
func (h *PixHandler) ListValores(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ValoresSugeridos)
}

// GenerateQR — GET /v1/pix/qr
func (h *PixHandler) GenerateQR(w http.ResponseWriter, r *http.Request) {
	valor := r.URL.Query().Get("valor")
	if valor == "" {
		valor = "1.00"
	}

	dataURI, payload, err := GerarQrPixB64(valor)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erro ao gerar QR Code PIX")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"qr_code": dataURI,
		"payload": payload,
	})
}

// Pix payload generation helpers
func crc16(payload string) string {
	data := []byte(payload)
	crc := 0xFFFF
	for _, b := range data {
		crc ^= int(b) << 8
		for i := 0; i < 8; i++ {
			if (crc & 0x8000) != 0 {
				crc = (crc << 1) ^ 0x1021
			} else {
				crc <<= 1
			}
			crc &= 0xFFFF
		}
	}
	return fmt.Sprintf("%04X", crc)
}

func field(id string, value string) string {
	return fmt.Sprintf("%s%02d%s", id, len(value), value)
}

func BuildPixPayload(chave, nome, cidade, valor string, txid string) string {
	if txid == "" {
		txid = "***"
	}
	gui := field("00", "br.gov.bcb.pix")
	key := field("01", chave)
	mai := field("26", gui+key)

	valFloat, _ := strconv.ParseFloat(valor, 64)
	valorFmt := fmt.Sprintf("%.2f", valFloat)
	transactionAmount := field("54", valorFmt)

	if len(nome) > 25 {
		nome = nome[:25]
	}
	if len(cidade) > 15 {
		cidade = cidade[:15]
	}
	merchantName := field("59", nome)
	merchantCity := field("60", cidade)

	txidField := field("05", txid)
	addData := field("62", txidField)

	payload := field("00", "01") + // Payload Format Indicator
		field("01", "12") + // Point of Initiation Method (12 = reutilizável)
		mai +
		field("52", "0000") + // Merchant Category Code
		field("53", "986") + // Transaction Currency (BRL)
		transactionAmount +
		field("58", "BR") + // Country Code
		merchantName +
		merchantCity +
		addData +
		"6304" // CRC placeholder

	return payload + crc16(payload)
}

func GerarQrPixB64(valor string) (string, string, error) {
	payload := BuildPixPayload(PixKey, PixName, PixCity, valor, "***")

	// Generate QR Code PNG bytes
	pngBytes, err := qrcode.Encode(payload, qrcode.Medium, 256)
	if err != nil {
		return "", "", err
	}

	b64 := base64.StdEncoding.EncodeToString(pngBytes)
	dataURI := fmt.Sprintf("data:image/png;base64,%s", b64)

	return dataURI, payload, nil
}
