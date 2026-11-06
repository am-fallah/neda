package report_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/migrations"
	"github.com/am-fallah/neda/internal/report"
	"github.com/am-fallah/neda/internal/store"
	_ "time/tzdata"
)

var tehran = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		panic(err)
	}
	return loc
}()

func newReportStore(t *testing.T) (*store.Store, *sql.DB) {
	t.Helper()
	database, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "report-test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	applied, err := db.Migrate(context.Background(), database.DB, migrations.FS)
	if err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	if err := database.Ready(context.Background(), applied); err != nil {
		t.Fatalf("database.Ready: %v", err)
	}
	return store.New(database.DB), database.DB
}

type reportFixture struct {
	store *store.Store
	db    *sql.DB
}

func (f reportFixture) target(t *testing.T) int64 {
	t.Helper()
	now := "2026-10-24T10:00:00Z"
	if _, err := f.db.Exec(`INSERT INTO monitors
		(id,label,origin_name,destination_name,departure_date,adults,children,infants,enabled,expires_at,created_at,updated_at)
		VALUES ('m-report','Daily report','Yazd','Mashhad','2026-11-20',1,0,0,1,'2026-11-21T23:59:59+03:30',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	result, err := f.db.Exec(`INSERT INTO monitor_providers
		(monitor_id,provider_id,active,origin_id,destination_id,config_version,created_at,updated_at)
		VALUES ('m-report','fake',1,'origin','destination',1,?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f reportFixture) event(t *testing.T, targetID int64, stateID int64, eventID, kind, occurredAt string) {
	t.Helper()
	var episodeID any
	if kind == "availability" {
		var seq int
		if err := f.db.QueryRow(`SELECT COALESCE(MAX(episode_seq), 0) + 1 FROM availability_episodes WHERE state_id = ?`, stateID).Scan(&seq); err != nil {
			t.Fatal(err)
		}
		result, err := f.db.Exec(`INSERT INTO availability_episodes (state_id, episode_seq, opened_at) VALUES (?, ?, ?)`, stateID, seq, occurredAt)
		if err != nil {
			t.Fatal(err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		episodeID = id
		if _, err := f.db.Exec(`UPDATE availability_episodes
			SET closed_at = ?, close_reason = 'not_reservable' WHERE id = ?`, occurredAt, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.Exec(`INSERT INTO events
		(id,kind,dedupe_key,monitor_id,provider_id,episode_id,occurred_at,origin_name,destination_name,train_number,wagon_name,class_name,created_at)
		VALUES (?,?,?,'m-report','fake',?,?, 'Yazd','Mashhad','101','Wagon 1','Economy',?)`,
		eventID, kind, "daily:"+eventID, episodeID, occurredAt, occurredAt); err != nil {
		t.Fatal(err)
	}
	_ = targetID
}

func (f reportFixture) check(t *testing.T, targetID int64, outcome, errorKind, startedAt string) {
	t.Helper()
	var finished any
	if outcome != "running" {
		finished = startedAt
	}
	if _, err := f.db.Exec(`INSERT INTO check_runs
		(monitor_provider_id,config_version,started_at,finished_at,outcome,error_kind,created_at)
		VALUES (?,1,?,?,?,?,?)`,
		targetID, startedAt, finished, outcome, nullable(errorKind), startedAt); err != nil {
		t.Fatal(err)
	}
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func TestBuildSummaryUsesTehranDayBoundariesAndJalaliDate(t *testing.T) {
	st, database := newReportStore(t)
	f := reportFixture{store: st, db: database}
	targetID := f.target(t)
	state, err := database.Exec(`INSERT INTO inventory_states
		(monitor_provider_id,config_version,journey_key,offering_key,state,state_since,last_observed_at,updated_at)
		VALUES (?,1,'j','o','reservable','2026-11-05T20:00:00Z','2026-11-05T20:00:00Z','2026-11-05T20:00:00Z')`, targetID)
	if err != nil {
		t.Fatal(err)
	}
	stateID, _ := state.LastInsertId()

	// 20:00Z is 23:30 Tehran on Nov 5; 21:00Z is 00:30 Tehran on Nov 6.
	f.event(t, targetID, stateID, "e-nov5", "availability", "2026-11-05T20:00:00Z")
	f.event(t, targetID, stateID, "e-nov6", "availability", "2026-11-05T21:00:00Z")
	f.check(t, targetID, "success", "", "2026-11-05T20:10:00Z")
	f.check(t, targetID, "error", "timeout", "2026-11-05T21:10:00Z")

	nov5 := time.Date(2026, 11, 5, 0, 0, 0, 0, tehran)
	summary, err := report.BuildSummary(context.Background(), database, st, nov5, nov5.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("BuildSummary Nov 5: %v", err)
	}
	if summary.Gregorian != "2026-11-05" || summary.Jalali != "1405-08-14" {
		t.Fatalf("Nov 5 dates = %s/%s", summary.Gregorian, summary.Jalali)
	}
	if summary.AvailabilityChanges != 1 || summary.ChecksTotal != 1 || summary.ChecksError != 0 {
		t.Fatalf("Nov 5 summary = %+v", summary)
	}

	nov6 := time.Date(2026, 11, 6, 0, 0, 0, 0, tehran)
	summary, err = report.BuildSummary(context.Background(), database, st, nov6, nov6.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("BuildSummary Nov 6: %v", err)
	}
	if summary.Jalali != "1405-08-15" || summary.AvailabilityChanges != 1 ||
		summary.ChecksTotal != 1 || summary.ChecksError != 1 || summary.ProviderErrors["timeout"] != 1 {
		t.Fatalf("Nov 6 summary = %+v", summary)
	}

	quietStart := time.Date(2026, 12, 1, 0, 0, 0, 0, tehran)
	quiet, err := report.BuildSummary(context.Background(), database, st, quietStart, quietStart.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("BuildSummary quiet: %v", err)
	}
	if !quiet.Quiet() {
		t.Fatalf("quiet summary = %+v", quiet)
	}
	text := report.FormatTelegram(quiet)
	if !strings.Contains(text, "Quiet day") || !strings.Contains(text, "1405-09-10") {
		t.Fatalf("quiet report text:\n%s", text)
	}
	if !strings.Contains(report.FormatTelegram(summary), "Provider errors: timeout 1") {
		t.Fatalf("error report text:\n%s", report.FormatTelegram(summary))
	}
}

func TestFormatTelegramEscapesAndStaysBounded(t *testing.T) {
	summary := report.Summary{
		Gregorian:           "2026-11-05",
		Jalali:              "1405-08-14",
		MonitoringEnabled:   true,
		ChecksTotal:         3,
		ChecksSuccess:       2,
		ChecksError:         1,
		AvailabilityLines:   []string{"Yazd → Mashhad · Train <101>"},
		ProviderErrors:      map[string]int{"<timeout>": 1},
		MonitorsTotal:       2,
		MonitorsActive:      1,
		MonitorsPaused:      1,
		MonitorsExpired:     0,
		AvailabilityChanges: 1,
	}
	text := report.FormatTelegram(summary)
	if strings.Contains(text, "<timeout>") || strings.Contains(text, "Train <101>") {
		t.Fatalf("report was not escaped:\n%s", text)
	}
	if !strings.Contains(text, "&lt;timeout&gt;") || !strings.Contains(text, "Train &lt;101&gt;") {
		t.Fatalf("escaped report:\n%s", text)
	}
	if len(text) > 4096 {
		t.Fatalf("report length = %d, want <= 4096", len(text))
	}
}

func TestFormatTelegramQuietDay(t *testing.T) {
	summary := report.Summary{Gregorian: "2026-11-05", Jalali: "1405-08-14"}
	if !strings.Contains(report.FormatTelegram(summary), "Quiet day") {
		t.Fatalf("quiet day text:\n%s", report.FormatTelegram(summary))
	}
}
