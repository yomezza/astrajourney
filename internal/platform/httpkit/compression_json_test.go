package httpkit

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConditionalJSONCompression(t *testing.T) {
	value := map[string]string{"body": strings.Repeat("Сообщение пилота. ", 100)}
	h := GzipJSON(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		ReplyConditional(w, r, value)
	}))
	request := func(tag, encoding string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/chat/api/v1/history", nil)
		r.Header.Set("Accept-Encoding", encoding)
		r.Header.Set("If-None-Match", tag)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	first := request("", "gzip")
	if first.Code != 200 || first.Header().Get("Content-Encoding") != "gzip" || first.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("compressed private response", first.Code, first.Header())
	}
	reader, err := gzip.NewReader(first.Body)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || !strings.Contains(string(decoded), "Сообщение пилота") {
		t.Fatal("invalid compressed JSON", err)
	}
	unchanged := request(first.Header().Get("ETag"), "gzip")
	if unchanged.Code != 304 || unchanged.Body.Len() != 0 || unchanged.Header().Get("Content-Encoding") != "" {
		t.Fatal("304 must have no gzip stream or body", unchanged.Code, unchanged.Header())
	}
	plain := request("", "gzip;q=0")
	if plain.Header().Get("Content-Encoding") != "" || !strings.Contains(plain.Body.String(), "Сообщение пилота") {
		t.Fatal("disabled gzip ignored")
	}
	value["body"] = "Новое сообщение"
	if changed := request(first.Header().Get("ETag"), "gzip"); changed.Code != 200 || changed.Header().Get("ETag") == first.Header().Get("ETag") {
		t.Fatal("changed view incorrectly returned 304")
	}
}

func TestJSONCompressionLeavesWebSocketWriterUntouched(t *testing.T) {
	w := httptest.NewRecorder()
	h := GzipJSON(http.HandlerFunc(func(got http.ResponseWriter, _ *http.Request) {
		if got != w {
			t.Fatal("upgrade writer was wrapped")
		}
	}))
	r := httptest.NewRequest("GET", "/chat/ws/v1", nil)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Accept-Encoding", "gzip")
	h.ServeHTTP(w, r)
}
