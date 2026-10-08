package httpapi

import (
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mingzaily/mailwake/internal/buildinfo"
	"github.com/mingzaily/mailwake/internal/storage"
)

func managementRoutes(api *gin.RouterGroup, monitor Runtime, store *storage.Store) {
	api.GET("/diagnostics", func(c *gin.Context) {
		counts, err := store.Summary(c.Request.Context())
		if err != nil {
			databaseError(c)
			return
		}
		// Use an explicit allowlist. Folder names, identities, message data and error
		// parameters stay in authenticated live views, except the IMAP response code.
		type watch struct {
			MailboxIndex int        `json:"mailbox_index"`
			Index        int        `json:"index"`
			Check        string     `json:"check"`
			State        string     `json:"state"`
			Mode         string     `json:"mode,omitempty"`
			LastCheck    *time.Time `json:"last_check,omitempty"`
			ErrorCode    string     `json:"error_code,omitempty"`
			ResponseCode string     `json:"response_code,omitempty"`
			NoticeCode   string     `json:"notice_code,omitempty"`
		}
		type mailbox struct {
			Index                  int    `json:"index"`
			NoticeCode             string `json:"notice_code,omitempty"`
			SubscriptionNoticeCode string `json:"subscription_notice_code,omitempty"`
		}
		boxes := []mailbox{}
		indexes, folderIndexes := map[string]int{}, map[string]int{}
		notices := monitor.Notices()
		for _, view := range monitor.Mailboxes() {
			id, _ := view["id"].(string)
			item := mailbox{Index: len(boxes) + 1}
			if notice := notices["mailbox:"+id]; notice != nil {
				item.NoticeCode = notice.Code
			}
			if notice := notices["subscriptions:"+id]; notice != nil {
				item.SubscriptionNoticeCode = notice.Code
			}
			indexes[id] = item.Index
			boxes = append(boxes, item)
		}
		watches := make([]watch, 0)
		for _, s := range monitor.Status() {
			if indexes[s.MailboxID] == 0 {
				indexes[s.MailboxID] = len(boxes) + 1
				boxes = append(boxes, mailbox{Index: len(boxes) + 1})
			}
			folderIndexes[s.MailboxID]++
			item := watch{MailboxIndex: indexes[s.MailboxID], Index: folderIndexes[s.MailboxID], Check: s.Check, State: s.State, Mode: s.Mode, LastCheck: s.LastCheck, NoticeCode: s.Notice}
			if s.LastError != nil {
				item.ErrorCode = s.LastError.Code
				if s.LastError.Code == "imap_folder_rejected" {
					item.ResponseCode = s.LastError.Params["response_code"]
				}
			}
			watches = append(watches, item)
		}
		version, revision := buildinfo.Current()
		c.Header("Content-Disposition", `attachment; filename="mailwake-diagnostics.json"`)
		c.JSON(200, gin.H{"schema_version": 1, "generated_at": time.Now().UTC(), "build": gin.H{"version": version, "revision": revision, "go_version": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH}, "mailboxes": boxes, "watches": watches, "delivery": counts})
	})
}
