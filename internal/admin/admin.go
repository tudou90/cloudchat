// Package admin implements `cloudchat admin …`, the moderation commands an
// operator runs on the server to inspect, preserve and remove reported
// rooms and secret notes. There is deliberately no web admin interface.
package admin

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"cloudchat/internal/database"
	"cloudchat/internal/models"
	"cloudchat/internal/service"
	"cloudchat/internal/transport/ws"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// ClosedNotice is what people in a room see when a moderator closes it.
const ClosedNotice = "This session was closed for violating our Terms of Service."

const usage = `Usage: cloudchat admin <command>

Rooms (accept an invite link or a room ID):
  room list                   all rooms: age, online, messages, files
  room show   <link>          who is in it, how many messages, which files
  room export <link> <dir>    save its messages and files to <dir> (evidence)
  room delete <link> [--yes]  delete it now and disconnect everyone in it

Secret notes (accept a note link or ID; their content can't be read):
  secret show   <link>
  secret delete <link> [--yes]

Usage statistics (daily totals, nothing personal):
  stats [days]                last 30 (or <days>) days: visitors, rooms, messages, files...
  stats archive               write finished days to STATS_FILE now

Run from the directory with .env (e.g. cd /opt/cloudchat && sudo ./cloudchat admin …).
`

// Options are the server settings admin commands report on.
type Options struct {
	MaxRooms  int    // MAX_ROOMS, 0 = unlimited
	StatsFile string // STATS_FILE, empty = statistics off
}

// Run executes an admin command; out receives the report, in answers prompts.
func Run(args []string, in io.Reader, out io.Writer, opts Options) error {
	if len(args) == 2 && args[0] == "room" && args[1] == "list" {
		return listRooms(out, opts)
	}
	if len(args) >= 1 && args[0] == "stats" {
		return showStats(args[1:], out, opts)
	}
	if len(args) < 2 {
		fmt.Fprint(out, usage)
		return errors.New("missing command")
	}
	yes := false
	var rest []string
	for _, a := range args[2:] {
		if a == "--yes" || a == "-y" {
			yes = true
		} else {
			rest = append(rest, a)
		}
	}
	confirm := func(what string) bool {
		if yes {
			return true
		}
		fmt.Fprintf(out, "%s This cannot be undone. Type 'yes' to continue: ", what)
		line, _ := bufio.NewReader(in).ReadString('\n')
		return strings.TrimSpace(line) == "yes"
	}

	switch args[0] + " " + args[1] {
	case "room show", "room export", "room delete":
		if len(rest) < 1 {
			return fmt.Errorf("room %s needs a room link or ID", args[1])
		}
		roomID, err := ParseRoom(rest[0])
		if err != nil {
			return err
		}
		switch args[1] {
		case "show":
			return showRoom(roomID, out)
		case "export":
			if len(rest) < 2 {
				return errors.New("room export needs a target directory")
			}
			return exportRoom(roomID, rest[1], out)
		default:
			if err := showRoom(roomID, out); err != nil {
				return err
			}
			if !confirm("\nDelete this room, its messages and files, and disconnect everyone in it?") {
				return errors.New("aborted")
			}
			existed, files, err := ws.CloseRoom(roomID, ClosedNotice)
			if err != nil {
				return err
			}
			if err := service.DeleteRoomFiles(roomID); err != nil {
				fmt.Fprintf(out, "Warning: removing the room's files from disk failed (the janitor will retry): %v\n", err)
			}
			if !existed {
				fmt.Fprintln(out, "Room data was already gone; any remaining connections were told to close.")
				return nil
			}
			fmt.Fprintf(out, "Deleted room %s (%d file(s)) and disconnected its members.\n", roomID, files)
			return nil
		}
	case "secret show", "secret delete":
		if len(rest) < 1 {
			return fmt.Errorf("secret %s needs a note link or ID", args[1])
		}
		id, err := ParseSecret(rest[0])
		if err != nil {
			return err
		}
		meta, err := service.GetSecretMeta(id)
		if errors.Is(err, service.ErrSecretNotFound) {
			fmt.Fprintf(out, "Secret %s: not found (already read, destroyed or expired).\n", id)
			return nil
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Secret %s: exists, expires in %s, %d password attempt(s) left.\n",
			id, time.Until(meta.ExpiresAt).Round(time.Minute), meta.AttemptsLeft)
		if args[1] == "show" {
			return nil
		}
		if !confirm("Delete this secret note?") {
			return errors.New("aborted")
		}
		if _, err := service.DeleteSecret(id); err != nil {
			return err
		}
		fmt.Fprintln(out, "Deleted.")
		return nil
	}
	fmt.Fprint(out, usage)
	return fmt.Errorf("unknown command %q", strings.Join(args[:2], " "))
}

// ParseRoom accepts an invite link (…/chat/?room=<id>) or a bare room ID.
func ParseRoom(s string) (string, error) {
	s = strings.TrimSpace(s)
	if u, err := url.Parse(s); err == nil && u.Query().Get("room") != "" {
		s = u.Query().Get("room")
	}
	if _, err := uuid.Parse(s); err != nil {
		return "", fmt.Errorf("%q is not a room link or room ID", s)
	}
	return strings.ToLower(s), nil
}

// ParseSecret accepts a note link (…/chat/secret/<id>#key) or a bare ID.
func ParseSecret(s string) (string, error) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "/secret/"); i >= 0 {
		s = strings.Trim(s[i+len("/secret/"):], "/")
	}
	if !service.ValidSecretID(s) {
		return "", fmt.Errorf("%q is not a secret note link or ID", s)
	}
	return s, nil
}

type roomFile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Mime string `json:"mime"`
	Size int64  `json:"size"`
}

type roomInfo struct {
	ID        string           `json:"id"`
	ExpiresIn string           `json:"expiresIn"`
	Online    []string         `json:"online"`
	Messages  []models.Message `json:"messages"`
	Files     []roomFile       `json:"files"`
}

func loadRoom(roomID string) (*roomInfo, error) {
	keys := ws.RoomKeys(roomID) // members, files, meta, history, ...
	ttl, err := database.RDB.PTTL(database.Ctx, keys[2]).Result()
	if err != nil {
		return nil, err
	}
	if ttl < 0 && ttl != -1 {
		return nil, fmt.Errorf("room %s not found (it may have ended and been deleted)", roomID)
	}
	info := &roomInfo{ID: roomID, ExpiresIn: ttl.Round(time.Second).String()}

	members, _ := database.RDB.HVals(database.Ctx, keys[0]).Result()
	seen := map[string]bool{}
	for _, v := range members {
		var m models.Member
		if json.Unmarshal([]byte(v), &m) == nil && !seen[m.ID] {
			seen[m.ID] = true
			info.Online = append(info.Online, m.Name)
		}
	}

	raw, _ := database.RDB.LRange(database.Ctx, keys[3], 0, -1).Result()
	for _, r := range raw {
		var m models.Message
		if json.Unmarshal([]byte(r), &m) == nil {
			info.Messages = append(info.Messages, m)
		}
	}

	ids, _ := database.RDB.SMembers(database.Ctx, keys[1]).Result()
	for _, id := range ids {
		vals, err := database.RDB.HMGet(database.Ctx, service.FileKeyPrefix(roomID)+id, "name", "mime", "size").Result()
		if err != nil || vals[0] == nil {
			continue // expired
		}
		size, _ := strconv.ParseInt(fmt.Sprint(vals[2]), 10, 64)
		info.Files = append(info.Files, roomFile{ID: id, Name: fmt.Sprint(vals[0]), Mime: fmt.Sprint(vals[1]), Size: size})
	}
	return info, nil
}

type roomSummary struct {
	id        string
	created   time.Time
	hasAge    bool
	expiresIn time.Duration
	online    int
	messages  int64
	files     int64
	fileBytes int64
}

func listRooms(out io.Writer, opts Options) error {
	ids, err := ws.ListRooms()
	if err != nil {
		return err
	}
	rooms := make([]roomSummary, 0, len(ids))
	for _, id := range ids {
		keys := ws.RoomKeys(id) // members, files, meta, history, filebytes, ...
		pipe := database.RDB.Pipeline()
		members := pipe.HVals(database.Ctx, keys[0])
		files := pipe.SCard(database.Ctx, keys[1])
		ttl := pipe.PTTL(database.Ctx, keys[2])
		msgs := pipe.LLen(database.Ctx, keys[3])
		bytes := pipe.Get(database.Ctx, keys[4])
		if _, err := pipe.Exec(database.Ctx); err != nil && !errors.Is(err, redis.Nil) {
			return err
		}
		if ttl.Val() < 0 && ttl.Val() != -1 {
			continue // expired while listing
		}
		r := roomSummary{id: id, expiresIn: ttl.Val(), messages: msgs.Val(), files: files.Val()}
		r.fileBytes, _ = bytes.Int64()
		r.created, r.hasAge = ws.RoomCreatedAt(id)
		seen := map[string]bool{}
		for _, v := range members.Val() {
			var m models.Member
			if json.Unmarshal([]byte(v), &m) == nil && !seen[m.ID] {
				seen[m.ID] = true
			}
		}
		r.online = len(seen)
		rooms = append(rooms, r)
	}
	// Busiest first, then newest.
	sort.Slice(rooms, func(i, j int) bool {
		if rooms[i].online != rooms[j].online {
			return rooms[i].online > rooms[j].online
		}
		return rooms[i].created.After(rooms[j].created)
	})

	limit := "unlimited"
	if opts.MaxRooms > 0 {
		limit = fmt.Sprintf("limit %d", opts.MaxRooms)
	}
	online := 0
	for _, r := range rooms {
		online += r.online
	}
	fmt.Fprintf(out, "%d room(s) (%s), %d person(s) online — times in %s\n", len(rooms), limit, online, time.Now().Format("MST"))
	if len(rooms) == 0 {
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "\nROOM\tCREATED\tAGE\tONLINE\tMESSAGES\tFILES\tDELETED IN")
	for _, r := range rooms {
		created, age := "?", "?"
		if r.hasAge {
			created = r.created.Local().Format("2006-01-02 15:04")
			age = shortDuration(time.Since(r.created))
		}
		files := strconv.FormatInt(r.files, 10)
		if r.files > 0 {
			files += fmt.Sprintf(" (%d KB)", (r.fileBytes+1023)/1024)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%s\t%s\n", r.id, created, age, r.online, r.messages, files, shortDuration(r.expiresIn))
	}
	return tw.Flush()
}

// shortDuration renders a duration as e.g. "45s", "12m", "3h20m", "2d4h".
func shortDuration(d time.Duration) string {
	if d < 0 {
		return "-"
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}

func showRoom(roomID string, out io.Writer) error {
	info, err := loadRoom(roomID)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Room %s\n", info.ID)
	fmt.Fprintf(out, "  Deleted in:  %s (unless there is activity)\n", info.ExpiresIn)
	fmt.Fprintf(out, "  Online now:  %d %s\n", len(info.Online), strings.Join(info.Online, ", "))
	fmt.Fprintf(out, "  Messages:    %d kept\n", len(info.Messages))
	fmt.Fprintf(out, "  Files:       %d\n", len(info.Files))
	for _, f := range info.Files {
		fmt.Fprintf(out, "    - %s  (%s, %d KB)\n", f.Name, f.Mime, (f.Size+1023)/1024)
	}
	return nil
}

func exportFile(roomID, fileID, dst string) error {
	_, src, err := service.OpenFile(roomID, fileID)
	if err != nil {
		return err
	}
	defer src.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func exportRoom(roomID, dir string, out io.Writer) error {
	info, err := loadRoom(roomID)
	if err != nil {
		return err
	}
	dir = filepath.Join(dir, "room-"+roomID+"-"+time.Now().UTC().Format("20060102T150405Z"))
	if err := os.MkdirAll(filepath.Join(dir, "files"), 0o700); err != nil {
		return err
	}
	record := struct {
		*roomInfo
		ExportedAt time.Time `json:"exportedAt"`
	}{info, time.Now().UTC()}
	data, _ := json.MarshalIndent(record, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "room.json"), data, 0o600); err != nil {
		return err
	}
	for _, f := range info.Files {
		name := f.ID + "-" + strings.NewReplacer("/", "_", "\\", "_").Replace(service.SanitizeFileName(f.Name))
		if err := exportFile(roomID, f.ID, filepath.Join(dir, "files", name)); err != nil {
			fmt.Fprintf(out, "  ! %s: %v\n", f.Name, err)
		}
	}
	fmt.Fprintf(out, "Exported %d message(s) and %d file(s) to %s\n", len(info.Messages), len(info.Files), dir)
	fmt.Fprintln(out, "This may contain illegal material: store it securely and only as long as the law requires.")
	return nil
}
