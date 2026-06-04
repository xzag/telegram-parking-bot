package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrSpotAlreadyBooked = errors.New("spot already booked")
	ErrUserAlreadyBooked = errors.New("user already has booking")
	ErrBookingNotFound   = errors.New("booking not found")
	ErrSpotNotFound      = errors.New("spot not found")
	ErrBookingClosed     = errors.New("booking is closed")
)

type Storage struct {
	db  *sql.DB
	loc *time.Location
}

type SpotState struct {
	SpotNumber string
	UserID     sql.NullInt64
	UserName   sql.NullString
	Username   sql.NullString
}

func New(path string) (*Storage, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)

	loc, err := time.LoadLocation("Asia/Novosibirsk")
	if err != nil {
		return nil, err
	}

	s := &Storage{
		db:  db,
		loc: loc,
	}

	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}

	return s, nil
}

func (s *Storage) Close() error {
	return s.db.Close()
}

func (s *Storage) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS users (
    telegram_id INTEGER PRIMARY KEY,
    username TEXT,
    name TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS spots (
    number TEXT PRIMARY KEY,
    active INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS bookings (
    id TEXT PRIMARY KEY,
    booking_date TEXT NOT NULL,
    spot_number TEXT NOT NULL REFERENCES spots(number),
    telegram_id INTEGER NOT NULL REFERENCES users(telegram_id),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,

    UNIQUE (booking_date, spot_number),
    UNIQUE (booking_date, telegram_id)
);

CREATE INDEX IF NOT EXISTS idx_bookings_date ON bookings(booking_date);
`)
	return err
}

func (s *Storage) SeedSpots(ctx context.Context, spots []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	for _, spot := range spots {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO spots(number, active)
VALUES (?, 1)
ON CONFLICT(number) DO UPDATE SET active = 1
`, spot); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Storage) BookToday(ctx context.Context, spotNumber string, telegramID int64, username string, name string) error {
	if !s.isBookingOpenNow() {
		return ErrBookingClosed
	}

	date := s.today()

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var spotExists int
	err = tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM spots WHERE number = ? AND active = 1
`, spotNumber).Scan(&spotExists)
	if err != nil {
		return err
	}
	if spotExists == 0 {
		return ErrSpotNotFound
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO users(telegram_id, username, name)
VALUES (?, ?, ?)
ON CONFLICT(telegram_id) DO UPDATE SET
    username = excluded.username,
    name = excluded.name
`, telegramID, username, name)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO bookings(id, booking_date, spot_number, telegram_id)
VALUES (?, ?, ?, ?)
`, fmt.Sprintf("%d-%s", time.Now().UnixNano(), spotNumber), date, spotNumber, telegramID)

	if err != nil {
		if isConstraintError(err) {
			var userSpot string

			err2 := tx.QueryRowContext(ctx, `
SELECT spot_number
FROM bookings
WHERE booking_date = ? AND telegram_id = ?
`, date, telegramID).Scan(&userSpot)

			if err2 == nil {
				return ErrUserAlreadyBooked
			}

			return ErrSpotAlreadyBooked
		}

		return err
	}

	return tx.Commit()
}

func (s *Storage) CancelToday(ctx context.Context, telegramID int64) (string, error) {
	date := s.today()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var spot string
	err = tx.QueryRowContext(ctx, `
SELECT spot_number
FROM bookings
WHERE booking_date = ? AND telegram_id = ?
`, date, telegramID).Scan(&spot)

	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrBookingNotFound
	}
	if err != nil {
		return "", err
	}

	_, err = tx.ExecContext(ctx, `
DELETE FROM bookings
WHERE booking_date = ? AND telegram_id = ?
`, date, telegramID)
	if err != nil {
		return "", err
	}

	return spot, tx.Commit()
}

func (s *Storage) TodayState(ctx context.Context) ([]SpotState, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT
    s.number,
    u.telegram_id,
    u.name,
    u.username
FROM spots s
LEFT JOIN bookings b
    ON b.spot_number = s.number
   AND b.booking_date = ?
LEFT JOIN users u
    ON u.telegram_id = b.telegram_id
WHERE s.active = 1
ORDER BY CAST(s.number AS INTEGER), s.number
`, s.today())
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	var result []SpotState

	for rows.Next() {
		var item SpotState
		if err := rows.Scan(
			&item.SpotNumber,
			&item.UserID,
			&item.UserName,
			&item.Username,
		); err != nil {
			return nil, err
		}
		result = append(result, item)
	}

	return result, rows.Err()
}

func (s *Storage) GetUserTodayBooking(ctx context.Context, telegramID int64) (string, bool, error) {
	var spot string

	err := s.db.QueryRowContext(ctx, `
SELECT spot_number
FROM bookings
WHERE booking_date = ? AND telegram_id = ?
`, s.today(), telegramID).Scan(&spot)

	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}

	return spot, true, nil
}

func (s *Storage) today() string {
	return time.Now().In(s.loc).Format("2006-01-02")
}

func (s *Storage) IsBookingOpenNow() bool {
	return s.isBookingOpenNow()
}

func (s *Storage) isBookingOpenNow() bool {
	now := time.Now().In(s.loc)
	openAt := time.Date(
		now.Year(),
		now.Month(),
		now.Day(),
		6,
		0,
		0,
		0,
		s.loc,
	)

	return !now.Before(openAt)
}

func isConstraintError(err error) bool {
	return err != nil && (contains(err.Error(), "constraint failed") ||
		contains(err.Error(), "UNIQUE constraint failed"))
}

func contains(s, sub string) bool {
	return len(sub) == 0 || len(s) >= len(sub) && stringContains(s, sub)
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
