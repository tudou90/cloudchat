package ws

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"cloudchat/internal/database"
	"github.com/redis/go-redis/v9"
)

// roomsKey is the set of room IDs, used to count rooms against MAX_ROOMS.
// Deleted and expired rooms are pruned from it lazily.
const roomsKey = "rooms"

// ErrRoomLimit means the platform already has MAX_ROOMS rooms.
var ErrRoomLimit = errors.New("room limit reached")

// createRoomLua counts live rooms (pruning ones that have expired or been
// deleted) and, if under the limit, creates the room and registers it.
// With no limit it only prunes a small random sample, keeping the set
// roughly the size of the live rooms at constant cost.
// KEYS: rooms set, new room's meta key. ARGV: room ID, TTL ms, max rooms,
// creation time (unix seconds).
var createRoomLua = redis.NewScript(`
local function live(id)
  if redis.call('EXISTS', 'room:' .. id .. ':meta') == 1 then return true end
  redis.call('SREM', KEYS[1], id)
  return false
end
local max = tonumber(ARGV[3])
if max > 0 then
  local n = 0
  for _, id in ipairs(redis.call('SMEMBERS', KEYS[1])) do
    if live(id) then n = n + 1 end
  end
  if n >= max then return 0 end
else
  for _, id in ipairs(redis.call('SRANDMEMBER', KEYS[1], 20)) do live(id) end
end
redis.call('SET', KEYS[2], ARGV[4], 'PX', ARGV[2])
redis.call('SADD', KEYS[1], ARGV[1])
return 1
`)

// CreateRoom creates a room that is deleted after ttl unless someone joins
// it. When maxRooms > 0 and the platform already has that many rooms, it
// returns ErrRoomLimit. The check and creation are atomic across servers.
func CreateRoom(roomID string, ttl time.Duration, maxRooms int) error {
	ok, err := createRoomLua.Run(database.Ctx, database.RDB, []string{roomsKey, RoomKey(roomID)},
		roomID, ttl.Milliseconds(), maxRooms, time.Now().Unix()).Int()
	if err != nil {
		return err
	}
	if ok == 0 {
		return ErrRoomLimit
	}
	return nil
}

// ListRooms returns the IDs of all existing rooms. It scans the keyspace, so
// it also finds rooms missing from the registry; meant for admin use.
func ListRooms() ([]string, error) {
	var ids []string
	iter := database.RDB.Scan(database.Ctx, 0, "room:*:meta", 1000).Iterator()
	for iter.Next(database.Ctx) {
		ids = append(ids, strings.TrimSuffix(strings.TrimPrefix(iter.Val(), "room:"), ":meta"))
	}
	return ids, iter.Err()
}

// SyncRoomRegistry adds existing rooms to the registry, e.g. rooms created
// by a version that didn't keep one, so MAX_ROOMS counts them.
func SyncRoomRegistry() error {
	ids, err := ListRooms()
	if err != nil || len(ids) == 0 {
		return err
	}
	members := make([]any, len(ids))
	for i, id := range ids {
		members[i] = id
	}
	return database.RDB.SAdd(database.Ctx, roomsKey, members...).Err()
}

// RoomCreatedAt reads when a room was created; ok is false for rooms made
// before creation times were recorded.
func RoomCreatedAt(roomID string) (t time.Time, ok bool) {
	v, err := database.RDB.Get(database.Ctx, RoomKey(roomID)).Result()
	if err != nil {
		return time.Time{}, false
	}
	sec, err := strconv.ParseInt(v, 10, 64)
	if err != nil || sec < 1_000_000_000 {
		return time.Time{}, false
	}
	return time.Unix(sec, 0), true
}

// CountRooms returns how many rooms are registered. The registry is pruned
// lazily, so this can run slightly above the number of live rooms.
func CountRooms() (int64, error) {
	return database.RDB.SCard(database.Ctx, roomsKey).Result()
}
