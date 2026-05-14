package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

var (
	instance *sql.DB
	once     sync.Once
	dbPath   = filepath.Join("data", "tstohls.db")
)

type Channel struct {
	ID          string `json:"id"`
	TvgID       string `json:"tvg_id,omitempty"`
	TvgName     string `json:"tvg_name,omitempty"`
	TvgLogo     string `json:"tvg_logo,omitempty"`
	Name        string `json:"name"`
	Logo        string `json:"logo"`
	Group       string `json:"group"`
	Url         string `json:"url"`
	VideoCodec  string `json:"video_codec,omitempty"`
	AudioCodec  string `json:"audio_codec,omitempty"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	FrameRate   string `json:"frame_rate,omitempty"`
	AudioSample int    `json:"audio_sample,omitempty"`
	InputFormat string `json:"input_format,omitempty"`
	Enabled     bool   `json:"enabled"`
	FailReason  string `json:"fail_reason,omitempty"`
}

const channelColumns = "id, tvg_id, tvg_name, tvg_logo, name, logo, grp, url, video_codec, audio_codec, width, height, frame_rate, audio_sample, input_format, enabled, fail_reason"

func Init() error {
	var initErr error
	once.Do(func() {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
			initErr = fmt.Errorf("创建数据目录失败: %w", err)
			return
		}

		instance, initErr = sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
		if initErr != nil {
			return
		}

		instance.SetMaxOpenConns(1)

		initErr = createTables()
	})
	return initErr
}

func createTables() error {
	_, err := instance.Exec(`
		CREATE TABLE IF NOT EXISTS channels (
			id            TEXT PRIMARY KEY,
			tvg_id        TEXT NOT NULL DEFAULT '',
			tvg_name      TEXT NOT NULL DEFAULT '',
			tvg_logo      TEXT NOT NULL DEFAULT '',
			name          TEXT NOT NULL DEFAULT '',
			logo          TEXT NOT NULL DEFAULT '',
			grp           TEXT NOT NULL DEFAULT '',
			url           TEXT NOT NULL,
			video_codec   TEXT NOT NULL DEFAULT '',
			audio_codec   TEXT NOT NULL DEFAULT '',
			width         INTEGER NOT NULL DEFAULT 0,
			height        INTEGER NOT NULL DEFAULT 0,
			frame_rate    TEXT NOT NULL DEFAULT '',
			audio_sample  INTEGER NOT NULL DEFAULT 0,
			input_format  TEXT NOT NULL DEFAULT '',
			enabled       INTEGER NOT NULL DEFAULT 1,
			fail_reason   TEXT NOT NULL DEFAULT '',
			created_at    DATETIME NOT NULL DEFAULT (datetime('now','localtime')),
			updated_at    DATETIME NOT NULL DEFAULT (datetime('now','localtime'))
		);

		CREATE INDEX IF NOT EXISTS idx_channels_grp     ON channels(grp);
		CREATE INDEX IF NOT EXISTS idx_channels_enabled  ON channels(enabled);
	`)

	_, _ = instance.Exec(`ALTER TABLE channels ADD COLUMN fail_reason TEXT NOT NULL DEFAULT ''`)

	_, err = instance.Exec(`
		CREATE TABLE IF NOT EXISTS source (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			url         TEXT NOT NULL DEFAULT '',
			file_path   TEXT NOT NULL DEFAULT '',
			created_at  DATETIME NOT NULL DEFAULT (datetime('now','localtime'))
		);
	`)
	return err
}

func Close() {
	if instance != nil {
		instance.Close()
	}
}

func InsertChannels(channels []Channel) error {
	tx, err := instance.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO channels
			(id, tvg_id, tvg_name, tvg_logo, name, logo, grp, url,
			 video_codec, audio_codec, width, height, frame_rate, audio_sample, input_format, enabled, fail_reason)
		VALUES
			(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
			 CASE WHEN ? THEN 1 ELSE 0 END,
			 ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, ch := range channels {
		_, err = stmt.Exec(
			ch.ID, ch.TvgID, ch.TvgName, ch.TvgLogo,
			ch.Name, ch.Logo, ch.Group, ch.Url,
			ch.VideoCodec, ch.AudioCodec,
			ch.Width, ch.Height, ch.FrameRate, ch.AudioSample, ch.InputFormat,
			ch.Enabled,
			ch.FailReason,
		)
		if err != nil {
			return fmt.Errorf("插入频道 %s 失败: %w", ch.ID, err)
		}
	}

	return tx.Commit()
}

func GetAllChannels() ([]Channel, error) {
	return queryChannels("SELECT " + channelColumns + " FROM channels ORDER BY id")
}

func GetEnabledChannels() ([]Channel, error) {
	return queryChannels("SELECT " + channelColumns + " FROM channels WHERE enabled = 1 ORDER BY id")
}

func queryChannels(query string, args ...any) ([]Channel, error) {
	rows, err := instance.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []Channel
	for rows.Next() {
		var ch Channel
		var enabled int
		if err := rows.Scan(
			&ch.ID, &ch.TvgID, &ch.TvgName, &ch.TvgLogo,
			&ch.Name, &ch.Logo, &ch.Group, &ch.Url,
			&ch.VideoCodec, &ch.AudioCodec,
			&ch.Width, &ch.Height, &ch.FrameRate, &ch.AudioSample, &ch.InputFormat,
			&enabled,
			&ch.FailReason,
		); err != nil {
			return nil, err
		}
		ch.Enabled = enabled == 1
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

func GetChannelByID(id string) (*Channel, error) {
	var ch Channel
	var enabled int
	err := instance.QueryRow(
		"SELECT "+channelColumns+" FROM channels WHERE id = ?", id,
	).Scan(
		&ch.ID, &ch.TvgID, &ch.TvgName, &ch.TvgLogo,
		&ch.Name, &ch.Logo, &ch.Group, &ch.Url,
		&ch.VideoCodec, &ch.AudioCodec,
		&ch.Width, &ch.Height, &ch.FrameRate, &ch.AudioSample, &ch.InputFormat,
		&enabled,
		&ch.FailReason,
	)
	if err != nil {
		return nil, err
	}
	ch.Enabled = enabled == 1
	return &ch, nil
}

func ToggleChannel(id string) (bool, error) {
	res, err := instance.Exec(
		"UPDATE channels SET enabled = CASE WHEN enabled = 1 THEN 0 ELSE 1 END, updated_at = datetime('now','localtime') WHERE id = ?",
		id,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return false, fmt.Errorf("频道 %s 不存在", id)
	}

	var enabled int
	instance.QueryRow("SELECT enabled FROM channels WHERE id = ?", id).Scan(&enabled)
	return enabled == 1, nil
}

func SetChannelEnabled(id string, enabled bool) error {
	e := 0
	if enabled {
		e = 1
	}
	res, err := instance.Exec(
		"UPDATE channels SET enabled = ?, updated_at = datetime('now','localtime') WHERE id = ?",
		e, id,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("频道 %s 不存在", id)
	}
	return nil
}

func DeleteAllChannels() error {
	_, err := instance.Exec("DELETE FROM channels")
	return err
}

func GetChannelCount() (int, error) {
	var count int
	err := instance.QueryRow("SELECT COUNT(*) FROM channels").Scan(&count)
	return count, err
}

func GetEnabledCount() (int, error) {
	var count int
	err := instance.QueryRow("SELECT COUNT(*) FROM channels WHERE enabled = 1").Scan(&count)
	return count, err
}

func GetGroups() ([]string, error) {
	rows, err := instance.Query("SELECT DISTINCT grp FROM channels WHERE grp != '' ORDER BY grp")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []string
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

func SaveSource(url, filePath string) error {
	_, err := instance.Exec(
		"INSERT OR REPLACE INTO source (id, url, file_path) VALUES (1, ?, ?)",
		url, filePath,
	)
	return err
}

func GetSource() (url string, filePath string, err error) {
	err = instance.QueryRow("SELECT url, file_path FROM source WHERE id = 1").Scan(&url, &filePath)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	return
}

func GetChannelsByFailReason(reason string) ([]Channel, error) {
	return queryChannels(
		"SELECT "+channelColumns+" FROM channels WHERE fail_reason = ? ORDER BY id", reason,
	)
}

func GetChannelsForProbing() ([]Channel, error) {
	return queryChannels(
		"SELECT " + channelColumns + " FROM channels WHERE fail_reason = '待后台探测' ORDER BY id",
	)
}

func UpdateChannelMeta(id string, ch Channel) error {
	enabled := 0
	if ch.Enabled {
		enabled = 1
	}
	_, err := instance.Exec(`
		UPDATE channels SET
			video_codec=?, audio_codec=?, width=?, height=?,
			frame_rate=?, audio_sample=?, input_format=?,
			enabled=?, fail_reason=?, updated_at=datetime('now','localtime')
		WHERE id=?`,
		ch.VideoCodec, ch.AudioCodec, ch.Width, ch.Height,
		ch.FrameRate, ch.AudioSample, ch.InputFormat,
		enabled, ch.FailReason, id,
	)
	return err
}

func GetProbingCount() (remaining int, total int, err error) {
	err = instance.QueryRow(
		"SELECT COUNT(*) FROM channels WHERE fail_reason = '待后台探测'",
	).Scan(&remaining)
	if err != nil {
		return
	}
	err = instance.QueryRow(
		"SELECT COUNT(*) FROM channels WHERE enabled = 0 AND fail_reason != ''",
	).Scan(&total)
	return
}
