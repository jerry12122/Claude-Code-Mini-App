package ws

import (
	"fmt"
	"strings"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/media"
)

// Only the runner receives filesystem paths. Persisted chat text stays readable.
func attachmentPrompt(database *db.DB, sessionID, text string, ids []string) (string, error) {
	attachments, err := database.ResolveAttachments(sessionID, ids)
	if err != nil {
		return "", err
	}
	var prompt strings.Builder
	for _, a := range attachments {
		p, err := media.ExistingUploadPath(a.SessionID, a.StorageName)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&prompt, "[附件] %s\n路徑：%s\n", a.Name, p)
	}
	prompt.WriteString(text)
	return prompt.String(), nil
}
