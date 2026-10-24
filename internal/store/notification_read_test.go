package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/store"
)

type readEventInput struct {
	id              string
	kind            notify.EventKind
	monitorID       string
	episodeID       *int64
	providerID      string
	occurredAt      time.Time
	trainNumber     string
	operator        string
	originName      string
	destinationName string
	departureAt     *time.Time
	arrivalAt       *time.Time
	wagonName       string
	className       string
	priceAmount     *int64
	priceCurrency   string
	bookingURL      string
	errorKind       string
	failureCount    *int
	createdAt       time.Time
}

func readNullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func readNullTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func readNullInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func readNullInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func insertReadEpisode(t *testing.T, database *sql.DB, targetID int64, journeyKey, offeringKey string, observedAt time.Time) int64 {
	t.Helper()
	result, err := database.Exec(`INSERT INTO inventory_states
		(monitor_provider_id, config_version, journey_key, offering_key, state,
		 state_since, last_observed_at, last_decisive_at, updated_at)
		VALUES (?, 1, ?, ?, 'reservable', ?, ?, ?, ?)`,
		targetID, journeyKey, offeringKey,
		formatTestTime(observedAt), formatTestTime(observedAt), formatTestTime(observedAt), formatTestTime(observedAt),
	)
	if err != nil {
		t.Fatalf("insert inventory state: %v", err)
	}
	stateID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("inventory state id: %v", err)
	}
	result, err = database.Exec(`INSERT INTO availability_episodes (state_id, episode_seq, opened_at)
		VALUES (?, 1, ?)`, stateID, formatTestTime(observedAt))
	if err != nil {
		t.Fatalf("insert availability episode: %v", err)
	}
	episodeID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("availability episode id: %v", err)
	}
	return episodeID
}

func insertReadEvent(t *testing.T, database *sql.DB, input readEventInput) {
	t.Helper()
	if input.createdAt.IsZero() {
		input.createdAt = input.occurredAt
	}
	if _, err := database.Exec(`INSERT INTO events
		(id, kind, dedupe_key, monitor_id, provider_id, episode_id, occurred_at,
		 train_number, operator, origin_name, destination_name, departure_at, arrival_at,
		 wagon_name, class_name, price_amount, price_currency, booking_url,
		 error_kind, failure_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		input.id, string(input.kind), "dedupe:"+input.id,
		readNullString(input.monitorID), input.providerID, readNullInt64(input.episodeID),
		formatTestTime(input.occurredAt), readNullString(input.trainNumber),
		readNullString(input.operator), readNullString(input.originName),
		readNullString(input.destinationName), readNullTime(input.departureAt),
		readNullTime(input.arrivalAt), readNullString(input.wagonName),
		readNullString(input.className), readNullInt64(input.priceAmount),
		readNullString(input.priceCurrency), readNullString(input.bookingURL),
		readNullString(input.errorKind), readNullInt(input.failureCount),
		formatTestTime(input.createdAt),
	); err != nil {
		t.Fatalf("insert event %s: %v", input.id, err)
	}
}

func insertReadDelivery(t *testing.T, database *sql.DB, id, eventID string, channel notify.Channel, targetKey string, status store.DeliveryStatus, attempts int, next, last, sent *time.Time, errorKind string, createdAt, updatedAt time.Time) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO notification_deliveries
		(id, event_id, channel, target_key, status, attempt_count, next_attempt_at,
		 last_attempt_at, sent_at, last_error_kind, lease_until, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?)`,
		id, eventID, string(channel), targetKey, string(status), attempts,
		readNullTime(next), readNullTime(last), readNullTime(sent),
		readNullString(errorKind), formatTestTime(createdAt), formatTestTime(updatedAt),
	); err != nil {
		t.Fatalf("insert delivery %s: %v", id, err)
	}
}

func TestNotificationReadModelEmptyAndValidation(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()

	events, err := st.ListEvents(ctx, store.EventQuery{})
	if err != nil {
		t.Fatalf("ListEvents empty: %v", err)
	}
	if events.Events == nil || len(events.Events) != 0 || events.NextCursor != "" {
		t.Fatalf("empty event page = %+v", events)
	}
	deliveries, err := st.ListDeliveries(ctx, store.DeliveryQuery{})
	if err != nil {
		t.Fatalf("ListDeliveries empty: %v", err)
	}
	if deliveries.Deliveries == nil || len(deliveries.Deliveries) != 0 || deliveries.NextCursor != "" {
		t.Fatalf("empty delivery page = %+v", deliveries)
	}
	if _, err := st.GetEvent(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetEvent missing error = %v, want ErrNotFound", err)
	}
	if _, err := st.GetEvent(ctx, " "); !errors.Is(err, store.ErrInvalidFilter) {
		t.Fatalf("GetEvent blank id error = %v, want ErrInvalidFilter", err)
	}

	tests := []struct {
		name  string
		query store.EventQuery
	}{
		{"negative limit", store.EventQuery{Limit: -1}},
		{"oversized limit", store.EventQuery{Limit: store.MaxReadLimit + 1}},
		{"unknown kind", store.EventQuery{Kind: "other"}},
		{"bad cursor", store.EventQuery{Cursor: "not-a-cursor"}},
	}
	for _, test := range tests {
		t.Run("events "+test.name, func(t *testing.T) {
			if _, err := st.ListEvents(ctx, test.query); err == nil {
				t.Fatal("ListEvents succeeded, want error")
			}
		})
	}

	deliveryTests := []struct {
		name  string
		query store.DeliveryQuery
	}{
		{"negative limit", store.DeliveryQuery{Limit: -1}},
		{"oversized limit", store.DeliveryQuery{Limit: store.MaxReadLimit + 1}},
		{"unknown channel", store.DeliveryQuery{Channel: "email"}},
		{"unknown status", store.DeliveryQuery{Status: "unknown"}},
		{"unknown kind", store.DeliveryQuery{Kind: "other"}},
		{"bad cursor", store.DeliveryQuery{Cursor: "not-a-cursor"}},
	}
	for _, test := range deliveryTests {
		t.Run("deliveries "+test.name, func(t *testing.T) {
			if _, err := st.ListDeliveries(ctx, test.query); err == nil {
				t.Fatal("ListDeliveries succeeded, want error")
			}
		})
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := st.ListEvents(canceled, store.EventQuery{}); err == nil {
		t.Fatal("ListEvents with canceled context succeeded")
	}
}

func TestNotificationReadModelOrderingPaginationAndFilters(t *testing.T) {
	st, database := newTestStore(t)
	ctx := context.Background()
	base := testNow

	monitor := createScheduleMonitor(t, st, "read-events", true, base.Add(24*time.Hour), nil,
		scheduleTargetSpec{providerID: "p-read", active: true})
	targetID := targetID(t, monitor, "p-read")

	episodeA := insertReadEpisode(t, database, targetID, "journey-a", "offering-a", base)
	episodeB := insertReadEpisode(t, database, targetID, "journey-b", "offering-b", base)
	episodeC := insertReadEpisode(t, database, targetID, "journey-c", "offering-c", base)
	episodeD := insertReadEpisode(t, database, targetID, "journey-d", "offering-d", base)
	episodeE := insertReadEpisode(t, database, targetID, "journey-e", "offering-e", base)

	availability := func(id string, episodeID int64, occurredAt time.Time, journey string) readEventInput {
		return readEventInput{
			id: id, kind: notify.KindAvailability, monitorID: monitor.ID,
			episodeID: &episodeID, providerID: string("p-read"),
			occurredAt: occurredAt, originName: "Tehran", destinationName: "Mashhad",
			trainNumber: "train-" + journey, wagonName: "wagon-" + journey,
			className: "class-" + journey, priceAmount: int64Ptr(125000),
			priceCurrency: "IRR", bookingURL: "https://mrbilit.com/" + journey,
		}
	}
	a := availability("evt-a", episodeA, base.Add(60*time.Second+900*time.Millisecond), "a")
	a.departureAt = timePtr(base.Add(48 * time.Hour))
	a.createdAt = a.occurredAt.Add(time.Second)
	insertReadEvent(t, database, a)
	insertReadEvent(t, database, availability("evt-b", episodeB, base.Add(60*time.Second+100*time.Millisecond), "b"))
	insertReadEvent(t, database, availability("evt-c", episodeC, base.Add(60*time.Second), "c"))
	insertReadEvent(t, database, availability("evt-e", episodeE, base.Add(30*time.Second), "e"))
	insertReadEvent(t, database, availability("evt-d", episodeD, base.Add(30*time.Second), "d"))
	failureCount := 3
	insertReadEvent(t, database, readEventInput{
		id: "evt-health", kind: notify.KindProviderDegraded, providerID: "p-other",
		occurredAt: base.Add(2 * time.Minute), errorKind: "timeout", failureCount: &failureCount,
	})

	all, err := st.ListEvents(ctx, store.EventQuery{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	wantOrder := []string{"evt-health", "evt-a", "evt-b", "evt-c", "evt-e", "evt-d"}
	if len(all.Events) != len(wantOrder) {
		t.Fatalf("events = %d, want %d: %+v", len(all.Events), len(wantOrder), all.Events)
	}
	for index, want := range wantOrder {
		if all.Events[index].ID != want {
			t.Fatalf("event %d = %q, want %q (chronological with id tie-break)", index, all.Events[index].ID, want)
		}
	}
	if all.NextCursor != "" {
		t.Fatalf("single-page cursor = %q, want empty", all.NextCursor)
	}
	healthEvent := all.Events[0]
	if healthEvent.MonitorID != "" || healthEvent.MonitorLabel != "" {
		t.Fatalf("provider-health event monitor identity = %q/%q, want empty", healthEvent.MonitorID, healthEvent.MonitorLabel)
	}
	if healthEvent.FailureCount == nil || *healthEvent.FailureCount != 3 || healthEvent.ErrorKind != "timeout" {
		t.Fatalf("provider-health detail = %+v", healthEvent)
	}
	aEvent := all.Events[1]
	if aEvent.MonitorID != monitor.ID || aEvent.MonitorLabel != monitor.Label || aEvent.EpisodeID == nil {
		t.Fatalf("availability identity = %+v", aEvent)
	}
	if aEvent.BookingURL != "https://mrbilit.com/a" || aEvent.PriceAmount == nil || *aEvent.PriceAmount != 125000 {
		t.Fatalf("availability snapshot = %+v", aEvent)
	}
	if aEvent.DepartureAt == nil || !aEvent.DepartureAt.Equal(base.Add(48*time.Hour)) {
		t.Fatalf("departureAt = %v", aEvent.DepartureAt)
	}
	if !aEvent.OccurredAt.Equal(base.Add(60*time.Second+900*time.Millisecond)) || !aEvent.CreatedAt.Equal(base.Add(61*time.Second+900*time.Millisecond)) {
		t.Fatalf("event timestamps = %v/%v", aEvent.OccurredAt, aEvent.CreatedAt)
	}

	// Walk pages of two and assert no duplicates, a stable order, and a cursor
	// only while more rows remain.
	var walked []string
	cursor := ""
	for pageNumber := 0; pageNumber < 4; pageNumber++ {
		page, err := st.ListEvents(ctx, store.EventQuery{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("ListEvents page %d: %v", pageNumber, err)
		}
		if page.Events == nil {
			t.Fatalf("page %d events is nil", pageNumber)
		}
		for _, event := range page.Events {
			walked = append(walked, event.ID)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(walked) != len(wantOrder) {
		t.Fatalf("walked %d events, want %d: %v", len(walked), len(wantOrder), walked)
	}
	for index, want := range wantOrder {
		if walked[index] != want {
			t.Fatalf("walked event %d = %q, want %q", index, walked[index], want)
		}
	}

	availabilityPage, err := st.ListEvents(ctx, store.EventQuery{Kind: notify.KindAvailability})
	if err != nil {
		t.Fatalf("ListEvents kind filter: %v", err)
	}
	if len(availabilityPage.Events) != 5 {
		t.Fatalf("availability events = %d, want 5", len(availabilityPage.Events))
	}
	healthPage, err := st.ListEvents(ctx, store.EventQuery{Kind: notify.KindProviderDegraded})
	if err != nil {
		t.Fatalf("ListEvents health kind: %v", err)
	}
	if len(healthPage.Events) != 1 || healthPage.Events[0].ID != "evt-health" {
		t.Fatalf("health page = %+v", healthPage.Events)
	}
	monitorPage, err := st.ListEvents(ctx, store.EventQuery{MonitorID: monitor.ID})
	if err != nil {
		t.Fatalf("ListEvents monitor filter: %v", err)
	}
	if len(monitorPage.Events) != 5 {
		t.Fatalf("monitor events = %d, want 5", len(monitorPage.Events))
	}
	providerPage, err := st.ListEvents(ctx, store.EventQuery{ProviderID: "p-read"})
	if err != nil {
		t.Fatalf("ListEvents provider filter: %v", err)
	}
	if len(providerPage.Events) != 5 {
		t.Fatalf("provider events = %d, want 5", len(providerPage.Events))
	}
}

func TestNotificationReadModelPerChannelDeliveryState(t *testing.T) {
	st, database := newTestStore(t)
	ctx := context.Background()
	base := testNow
	monitor := createScheduleMonitor(t, st, "read-delivery", true, base.Add(24*time.Hour), nil,
		scheduleTargetSpec{providerID: "p-delivery", active: true})
	targetID := targetID(t, monitor, "p-delivery")
	episodeID := insertReadEpisode(t, database, targetID, "journey", "offering", base)
	insertReadEvent(t, database, readEventInput{
		id: "evt-delivery", kind: notify.KindAvailability, monitorID: monitor.ID,
		episodeID: &episodeID, providerID: "p-delivery", occurredAt: base,
	})

	next := base.Add(2 * time.Minute)
	last := base.Add(time.Minute)
	sent := base.Add(time.Minute)
	insertReadDelivery(t, database, "d-pending", "evt-delivery", notify.ChannelTelegram, "telegram",
		store.DeliveryPending, 0, &base, nil, nil, "", base, base)
	insertReadDelivery(t, database, "d-sending", "evt-delivery", notify.ChannelPush, "push:one",
		store.DeliverySending, 1, nil, &last, nil, "", base, last)
	insertReadDelivery(t, database, "d-sent", "evt-delivery", notify.ChannelPush, "push:two",
		store.DeliverySent, 1, nil, &last, &sent, "", base, sent)
	insertReadDelivery(t, database, "d-retry", "evt-delivery", notify.ChannelPush, "push:three",
		store.DeliveryRetryScheduled, 2, &next, &last, nil, "telegram_transient", base, last)
	insertReadDelivery(t, database, "d-failed", "evt-delivery", notify.ChannelPush, "push:four",
		store.DeliveryFailed, 5, nil, &last, nil, "permanent", base, last)
	insertReadDelivery(t, database, "d-skipped", "evt-delivery", notify.ChannelPush, "push:five",
		store.DeliverySkipped, 0, nil, nil, nil, "monitor_expired", base, base)

	event, err := st.GetEvent(ctx, "evt-delivery")
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if len(event.Deliveries) != 6 {
		t.Fatalf("deliveries = %d, want 6", len(event.Deliveries))
	}
	statuses := make(map[store.DeliveryStatus]int)
	channels := make(map[notify.Channel]int)
	for _, delivery := range event.Deliveries {
		statuses[delivery.Status]++
		channels[delivery.Channel]++
	}
	for _, status := range []store.DeliveryStatus{
		store.DeliveryPending, store.DeliverySending, store.DeliverySent,
		store.DeliveryRetryScheduled, store.DeliveryFailed, store.DeliverySkipped,
	} {
		if statuses[status] != 1 {
			t.Fatalf("status %q count = %d, want 1", status, statuses[status])
		}
	}
	if channels[notify.ChannelTelegram] != 1 || channels[notify.ChannelPush] != 5 {
		t.Fatalf("channel counts = %+v", channels)
	}
	summary := event.DeliverySummary
	if summary.Total != 6 || summary.Pending != 1 || summary.Sending != 1 || summary.Sent != 1 ||
		summary.RetryScheduled != 1 || summary.Failed != 1 || summary.Skipped != 1 {
		t.Fatalf("delivery summary = %+v", summary)
	}
	if summary.Attempted != 4 {
		t.Fatalf("attempted = %d, want 4", summary.Attempted)
	}
	if summary.Terminal != 3 {
		t.Fatalf("terminal = %d, want 3", summary.Terminal)
	}
	for _, delivery := range event.Deliveries {
		if delivery.Channel == notify.ChannelTelegram && delivery.AttemptCount != 0 {
			t.Fatalf("pending delivery attempts = %d, want 0", delivery.AttemptCount)
		}
		if delivery.Status == store.DeliveryRetryScheduled {
			if delivery.NextAttemptAt == nil || !delivery.NextAttemptAt.Equal(next) ||
				delivery.LastErrorKind != "telegram_transient" || delivery.AttemptCount != 2 {
				t.Fatalf("retry delivery = %+v", delivery)
			}
		}
	}

	// A second read must produce the same delivery order and summary.
	again, err := st.GetEvent(ctx, "evt-delivery")
	if err != nil {
		t.Fatalf("GetEvent again: %v", err)
	}
	for index := range event.Deliveries {
		if event.Deliveries[index].ID != again.Deliveries[index].ID {
			t.Fatalf("delivery order changed at %d: %q/%q", index, event.Deliveries[index].ID, again.Deliveries[index].ID)
		}
	}
}

func TestNotificationReadModelDeliveryListFiltersAndPagination(t *testing.T) {
	st, database := newTestStore(t)
	ctx := context.Background()
	base := testNow
	monitor := createScheduleMonitor(t, st, "read-history", true, base.Add(24*time.Hour), nil,
		scheduleTargetSpec{providerID: "p-history", active: true})
	targetID := targetID(t, monitor, "p-history")
	episodeID := insertReadEpisode(t, database, targetID, "journey-history", "offering-history", base)
	insertReadEvent(t, database, readEventInput{
		id: "evt-history", kind: notify.KindAvailability, monitorID: monitor.ID,
		episodeID: &episodeID, providerID: "p-history", occurredAt: base,
	})
	insertReadEvent(t, database, readEventInput{
		id: "evt-health-history", kind: notify.KindProviderRecovered, providerID: "p-history",
		occurredAt: base.Add(time.Minute), errorKind: "timeout", failureCount: intPtr(4),
	})

	channels := []notify.Channel{notify.ChannelTelegram, notify.ChannelPush, notify.ChannelPush, notify.ChannelPush}
	targets := []string{"telegram", "push:history-b", "push:history-c", "push:history-d"}
	for index := 0; index < 4; index++ {
		created := base.Add(time.Duration(index) * time.Second)
		insertReadDelivery(t, database, "history-"+string(rune('a'+index)), "evt-history",
			channels[index], targets[index], store.DeliverySent, index+1, nil, &created, &created,
			"", created, created)
	}
	insertReadDelivery(t, database, "history-health", "evt-health-history", notify.ChannelPush, "push:one",
		store.DeliveryFailed, 5, nil, &base, nil, "permanent", base.Add(time.Minute), base.Add(time.Minute))

	all, err := st.ListDeliveries(ctx, store.DeliveryQuery{})
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	wantAll := []string{"history-health", "history-d", "history-c", "history-b", "history-a"}
	if len(all.Deliveries) != len(wantAll) {
		t.Fatalf("deliveries = %d, want %d: %+v", len(all.Deliveries), len(wantAll), all.Deliveries)
	}
	for index, want := range wantAll {
		if all.Deliveries[index].ID != want {
			t.Fatalf("delivery %d = %q, want %q (created_at desc with id tie-break)", index, all.Deliveries[index].ID, want)
		}
	}
	first := all.Deliveries[0]
	if first.EventKind != notify.KindProviderRecovered || first.ProviderID != "p-history" || first.MonitorID != "" {
		t.Fatalf("health delivery event context = %+v", first)
	}
	availabilityDelivery := all.Deliveries[1]
	if availabilityDelivery.EventKind != notify.KindAvailability || availabilityDelivery.MonitorID != monitor.ID {
		t.Fatalf("availability delivery event context = %+v", availabilityDelivery)
	}
	if !availabilityDelivery.EventOccurredAt.Equal(base) || !availabilityDelivery.CreatedAt.Equal(base.Add(3*time.Second)) {
		t.Fatalf("delivery timestamps = %v/%v", availabilityDelivery.EventOccurredAt, availabilityDelivery.CreatedAt)
	}

	var walked []string
	cursor := ""
	for pageNumber := 0; pageNumber < 4; pageNumber++ {
		page, err := st.ListDeliveries(ctx, store.DeliveryQuery{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("ListDeliveries page %d: %v", pageNumber, err)
		}
		for _, delivery := range page.Deliveries {
			walked = append(walked, delivery.ID)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(walked) != len(wantAll) {
		t.Fatalf("walked deliveries = %v, want %v", walked, wantAll)
	}
	for index, want := range wantAll {
		if walked[index] != want {
			t.Fatalf("walked delivery %d = %q, want %q", index, walked[index], want)
		}
	}

	telegram, err := st.ListDeliveries(ctx, store.DeliveryQuery{Channel: notify.ChannelTelegram})
	if err != nil {
		t.Fatalf("ListDeliveries channel: %v", err)
	}
	if len(telegram.Deliveries) != 1 {
		t.Fatalf("telegram deliveries = %d, want 1", len(telegram.Deliveries))
	}
	pushPage, err := st.ListDeliveries(ctx, store.DeliveryQuery{Channel: notify.ChannelPush})
	if err != nil {
		t.Fatalf("ListDeliveries push channel: %v", err)
	}
	if len(pushPage.Deliveries) != 4 {
		t.Fatalf("push deliveries = %d, want 4", len(pushPage.Deliveries))
	}
	sentPage, err := st.ListDeliveries(ctx, store.DeliveryQuery{Status: store.DeliverySent})
	if err != nil {
		t.Fatalf("ListDeliveries status: %v", err)
	}
	if len(sentPage.Deliveries) != 4 {
		t.Fatalf("sent deliveries = %d, want 4", len(sentPage.Deliveries))
	}
	eventPage, err := st.ListDeliveries(ctx, store.DeliveryQuery{EventID: "evt-health-history"})
	if err != nil {
		t.Fatalf("ListDeliveries event: %v", err)
	}
	if len(eventPage.Deliveries) != 1 || eventPage.Deliveries[0].EventKind != notify.KindProviderRecovered {
		t.Fatalf("event delivery page = %+v", eventPage.Deliveries)
	}
	healthKind, err := st.ListDeliveries(ctx, store.DeliveryQuery{Kind: notify.KindProviderRecovered})
	if err != nil {
		t.Fatalf("ListDeliveries kind: %v", err)
	}
	if len(healthKind.Deliveries) != 1 || healthKind.Deliveries[0].ID != "history-health" {
		t.Fatalf("kind delivery page = %+v", healthKind.Deliveries)
	}
	empty, err := st.ListDeliveries(ctx, store.DeliveryQuery{Status: store.DeliverySkipped})
	if err != nil {
		t.Fatalf("ListDeliveries empty filter: %v", err)
	}
	if empty.Deliveries == nil || len(empty.Deliveries) != 0 {
		t.Fatalf("empty filtered page = %+v", empty)
	}
}

func TestNotificationReadModelMonitorDeletionCascades(t *testing.T) {
	st, database := newTestStore(t)
	ctx := context.Background()
	base := testNow
	monitor := createScheduleMonitor(t, st, "read-cascade", true, base.Add(24*time.Hour), nil,
		scheduleTargetSpec{providerID: "p-cascade", active: true})
	targetID := targetID(t, monitor, "p-cascade")
	episodeID := insertReadEpisode(t, database, targetID, "journey-cascade", "offering-cascade", base)
	insertReadEvent(t, database, readEventInput{
		id: "evt-cascade", kind: notify.KindAvailability, monitorID: monitor.ID,
		episodeID: &episodeID, providerID: "p-cascade", occurredAt: base,
	})
	insertReadDelivery(t, database, "delivery-cascade", "evt-cascade", notify.ChannelTelegram, "telegram",
		store.DeliveryPending, 0, &base, nil, nil, "", base, base)
	insertReadEvent(t, database, readEventInput{
		id: "evt-cascade-health", kind: notify.KindProviderDegraded, providerID: "p-cascade",
		occurredAt: base.Add(time.Minute), errorKind: "timeout", failureCount: intPtr(3),
	})

	if err := st.DeleteMonitor(ctx, monitor.ID); err != nil {
		t.Fatalf("DeleteMonitor: %v", err)
	}
	if _, err := st.GetEvent(ctx, "evt-cascade"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted availability event error = %v, want ErrNotFound", err)
	}
	deliveries, err := st.ListDeliveries(ctx, store.DeliveryQuery{EventID: "evt-cascade"})
	if err != nil {
		t.Fatalf("ListDeliveries after delete: %v", err)
	}
	if len(deliveries.Deliveries) != 0 {
		t.Fatalf("cascade deliveries = %+v, want none", deliveries.Deliveries)
	}
	events, err := st.ListEvents(ctx, store.EventQuery{})
	if err != nil {
		t.Fatalf("ListEvents after delete: %v", err)
	}
	if len(events.Events) != 1 || events.Events[0].ID != "evt-cascade-health" {
		t.Fatalf("events after delete = %+v", events.Events)
	}
}

func int64Ptr(value int64) *int64 {
	return &value
}
