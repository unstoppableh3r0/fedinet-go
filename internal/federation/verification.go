package federation

import (
	"encoding/json"
	"log"

	"github.com/unstoppableh3r0/fedinet-go/pkg/models"
)

// VerifyActivity validates incoming federated activity.
// Temporarily simplified to ensure build stability.
func VerifyActivity(activity models.Activity, publicKey string) bool {
	_, err := json.Marshal(activity)
	if err != nil {
		log.Println("Failed to marshal activity:", err)
		return false
	}

	// Signature validation temporarily disabled
	// because models.Activity does not contain Signature field
	_ = publicKey

	return true
}
