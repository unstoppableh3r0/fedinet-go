package aimoderation

import "os"

func GetAPIKey() string {
	return os.Getenv("AI_MODERATION_API_KEY")
}
