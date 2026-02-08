package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Mock DB or dependencies would be needed for a true unit test of the handler
// since it calls GetIdentityByUserID and interacts with DB.
// However, we can at least test the request validation logic.

func TestRecoverAccountHandler_Validation(t *testing.T) {
	tests := []struct {
		name       string
		payload    map[string]string
		wantStatus int
	}{
		{
			name:       "Missing Fields",
			payload:    map[string]string{"user_id": "foo"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "Empty UserID",
			payload:    map[string]string{"user_id": "", "recovery_key": "abc"},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.payload)
			req, _ := http.NewRequest("POST", "/recover", bytes.NewBuffer(body))
			rr := httptest.NewRecorder()

			RecoverAccountHandler(rr, req)

			if status := rr.Code; status != tt.wantStatus {
				t.Errorf("handler returned wrong status code: got %v want %v",
					status, tt.wantStatus)
			}
		})
	}
}
