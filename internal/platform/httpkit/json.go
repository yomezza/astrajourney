package httpkit

import (
	"encoding/json"
	"github.com/yomezza/cosmosimulator/internal/platform/telemetry"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

func Reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func Fail(w http.ResponseWriter, status int, s string) {
	Reply(w, status, map[string]string{"error": s})
}

func FailCode(w http.ResponseWriter, status int, code, message string) {
	Reply(w, status, map[string]string{"error": message, "code": code})
}

func Decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, jsonBodyLimit(r))
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		Fail(w, 400, "Некорректный JSON")
		return false
	}
	return true
}

func Internal(w http.ResponseWriter, err error) {
	if strings.Contains(err.Error(), "Корабль закреплён за планетарным визитом") {
		Fail(w, 409, "Корабль закреплён за планетарным визитом")
		return
	}
	if strings.Contains(err.Error(), "Сумма превышает технический диапазон") {
		Fail(w, 409, "Сумма превышает технический диапазон")
		return
	}
	slog.Warn("request failed", telemetry.ErrorFields(err)...)
	Fail(w, 500, "Внутренняя ошибка сервера")
}
