package mail

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mingzaily/mailwake/internal/fault"
)

// Subscriptions is the durable desired state for one mailbox.
type Subscriptions struct {
	Revision int64    `json:"revision"`
	Folders  []Folder `json:"folders"`
}

func NormalizeFolders(folders []string) ([]string, error) {
	result := make([]string, 0, len(folders))
	seen := make(map[string]bool)
	for _, folder := range folders {
		if strings.EqualFold(folder, "INBOX") {
			folder = "INBOX"
		}
		if folder == "" || len(folder) > 4096 || !utf8.ValidString(folder) || strings.ContainsAny(folder, "\r\n\x00") || seen[folder] {
			return nil, fault.New("config_folders_invalid")
		}
		seen[folder] = true
		result = append(result, folder)
	}
	return result, nil
}

const Realtime = "realtime"

type Folder struct {
	Name  string `json:"name"`
	Check string `json:"check"`
}

func (f Folder) Interval() time.Duration {
	switch f.Check {
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	}
	return 0
}
func RealtimeFolders(names []string) []Folder {
	result := make([]Folder, 0, len(names))
	for _, name := range names {
		result = append(result, Folder{Name: name, Check: Realtime})
	}
	return result
}
func FolderNames(folders []Folder) []string {
	result := make([]string, 0, len(folders))
	for _, f := range folders {
		result = append(result, f.Name)
	}
	return result
}
func NormalizeSubscriptions(folders []Folder) ([]Folder, error) {
	names, err := NormalizeFolders(FolderNames(folders))
	if err != nil {
		return nil, err
	}
	result := make([]Folder, 0, len(folders))
	for i, f := range folders {
		if f.Check != Realtime && f.Check != "5m" && f.Check != "15m" {
			return nil, fault.New("folder_check_invalid")
		}
		f.Name = names[i]
		result = append(result, f)
	}
	return result, nil
}
func RequiredConnections(folders []Folder) int {
	realtime, scheduled := 0, 0
	for _, f := range folders {
		if f.Check == Realtime {
			realtime++
		} else {
			scheduled = 1
		}
	}
	return realtime + scheduled
}
