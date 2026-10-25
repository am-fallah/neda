package mrbilit

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/provider"
)

func searchTestProvider(t *testing.T, pageHandler, apiHandler http.Handler) *Provider {
	t.Helper()
	page := httptest.NewServer(pageHandler)
	t.Cleanup(page.Close)
	api := httptest.NewServer(apiHandler)
	t.Cleanup(api.Close)
	pageURL, _ := url.Parse(page.URL)
	apiURL, _ := url.Parse(api.URL)
	adapter, err := New(Config{
		BaseURL:           apiURL,
		SearchPageBaseURL: pageURL,
		NoRetries:         true,
		Now: func() time.Time {
			return time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return adapter
}

func observedFixture(t *testing.T) []byte {
	t.Helper()
	payload, err := os.ReadFile("../../../testdata/mrbilit/observed/sanitized-live-2026-11-05.json")
	if err != nil {
		t.Fatalf("read observed fixture: %v", err)
	}
	return payload
}

func validSearchPage(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/trains/yazd-mashhad" {
			t.Errorf("page path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("departureDate"); got != "1405-08-14" {
			t.Errorf("page departureDate = %q", got)
		}
		if got := r.URL.Query().Get("utm_source"); got != "" {
			t.Errorf("page kept tracking parameter = %q", got)
		}
		if got := r.Header.Get("Cookie"); got != "" {
			t.Errorf("page cookie = %q", got)
		}
		fixture, err := os.ReadFile("../../../testdata/mrbilit/search-page/valid.html")
		if err != nil {
			t.Errorf("read page fixture: %v", err)
			http.Error(w, "fixture unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(fixture)
	})
}

func validSearchAPI(t *testing.T, fixture []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/GetAvailable/v2" {
			t.Errorf("api path = %q", r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("from") != "219" || query.Get("to") != "191" {
			t.Errorf("route query = %s", r.URL.RawQuery)
		}
		if query.Get("date") != "2026-11-05" {
			t.Errorf("date = %q", query.Get("date"))
		}
		if query.Get("adultCount") != "1" || query.Get("childCount") != "0" || query.Get("infantCount") != "0" {
			t.Errorf("passenger query = %s", r.URL.RawQuery)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("authorization header = %q", got)
		}
		if got := r.Header.Get("Cookie"); got != "" {
			t.Errorf("cookie = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	})
}

func TestSearchURLResolvesAndNormalizes(t *testing.T) {
	adapter := searchTestProvider(t,
		validSearchPage(t),
		validSearchAPI(t, observedFixture(t)),
	)
	result, err := adapter.SearchURL(context.Background(), provider.SearchRequest{
		URL:        "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-14&utm_source=test",
		Passengers: provider.Passengers{Adults: 1},
	})
	if err != nil {
		t.Fatalf("SearchURL: %v", err)
	}
	if result.URL != "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-14" {
		t.Fatalf("canonical URL = %q", result.URL)
	}
	if result.DepartureDate != "2026-11-05" || result.JalaliDate != "1405-08-14" {
		t.Fatalf("dates = %q/%q", result.JalaliDate, result.DepartureDate)
	}
	if result.Route.OriginID != "219" || result.Route.DestinationID != "191" {
		t.Fatalf("route = %+v", result.Route)
	}
	if result.Route.OriginName != "Yazd" || result.Route.DestinationName != "Mashhad" {
		t.Fatalf("route names = %+v", result.Route)
	}
	if len(result.Result.Journeys) != 1 || len(result.Result.Journeys[0].Offerings) != 6 {
		t.Fatalf("result = %+v", result.Result)
	}
	if result.Result.Journeys[0].Offerings[1].Offering.WagonID != "9202" {
		t.Fatalf("wagon id = %q", result.Result.Journeys[0].Offerings[1].Offering.WagonID)
	}
}

func TestSearchURLUsesUserExampleDate(t *testing.T) {
	parsed, err := parseSearchURL("https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-24")
	if err != nil {
		t.Fatalf("parseSearchURL: %v", err)
	}
	if got := parsed.gregorianDate.Format(searchDateLayout); got != "2026-11-15" {
		t.Fatalf("gregorian = %q", got)
	}
}

func TestSearchURLAcceptsOneTrailingSlash(t *testing.T) {
	parsed, err := parseSearchURL("https://mrbilit.com/trains/yazd-mashhad/?departureDate=1405-08-24")
	if err != nil {
		t.Fatalf("parseSearchURL: %v", err)
	}
	if parsed.slug != "yazd-mashhad" {
		t.Fatalf("slug = %q", parsed.slug)
	}
}

func TestSearchURLRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		url  string
		code string
	}{
		{"empty", "", "url_required"},
		{"http", "http://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-14", "unsupported_url"},
		{"host", "https://example.com/trains/yazd-mashhad?departureDate=1405-08-14", "unsupported_url"},
		{"path", "https://mrbilit.com/train/yazd-mashhad?departureDate=1405-08-14", "unsupported_url"},
		{"extra path", "https://mrbilit.com/trains/yazd-mashhad/extra?departureDate=1405-08-14", "unsupported_url"},
		{"missing date", "https://mrbilit.com/trains/yazd-mashhad", "missing_date"},
		{"duplicate date", "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-14&departureDate=1405-08-15", "missing_date"},
		{"bad date", "https://mrbilit.com/trains/yazd-mashhad?departureDate=not-a-date", "invalid_date"},
		{"impossible date", "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-13-01", "invalid_date"},
		{"credentials", "https://user:pass@mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-14", "unsupported_url"},
		{"fragment", "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-14#x", "unsupported_url"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseSearchURL(test.url)
			var input *provider.InputError
			if !errors.As(err, &input) || input.Code != test.code {
				t.Fatalf("error = %v, want code %q", err, test.code)
			}
		})
	}
}

func TestSearchURLRejectsPastDateBeforeNetwork(t *testing.T) {
	var calls atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	})
	adapter := searchTestProvider(t, handler, handler)
	_, err := adapter.SearchURL(context.Background(), provider.SearchRequest{
		URL:        "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-07-28",
		Passengers: provider.Passengers{Adults: 1},
	})
	var input *provider.InputError
	if !errors.As(err, &input) || input.Code != "past_date" {
		t.Fatalf("error = %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("network calls = %d, want 0", calls.Load())
	}
}

func TestSearchURLRouteResolutionFailure(t *testing.T) {
	page := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html>no route data</html>`)
	})
	adapter := searchTestProvider(t, page, validSearchAPI(t, observedFixture(t)))
	_, err := adapter.SearchURL(context.Background(), provider.SearchRequest{
		URL:        "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-14",
		Passengers: provider.Passengers{Adults: 1},
	})
	var input *provider.InputError
	if !errors.As(err, &input) || input.Code != "route_resolution_failed" {
		t.Fatalf("error = %v", err)
	}
}

func TestSearchURLRedirectRejected(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/other", http.StatusFound)
	}))
	t.Cleanup(page.Close)
	api := httptest.NewServer(validSearchAPI(t, observedFixture(t)))
	t.Cleanup(api.Close)
	pageURL, _ := url.Parse(page.URL)
	apiURL, _ := url.Parse(api.URL)
	adapter, err := New(Config{
		BaseURL:           apiURL,
		SearchPageBaseURL: pageURL,
		NoRetries:         true,
		Now: func() time.Time {
			return time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.SearchURL(context.Background(), provider.SearchRequest{
		URL:        "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-14",
		Passengers: provider.Passengers{Adults: 1},
	})
	var providerErr *provider.Error
	if !errors.As(err, &providerErr) || providerErr.Kind != provider.ErrInvalidResponse {
		t.Fatalf("error = %v", err)
	}
}

func TestSearchURLPageTimeoutIsProviderTimeout(t *testing.T) {
	page := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	adapter := searchTestProvider(t, page, validSearchAPI(t, observedFixture(t)))
	adapter.pageClient.Timeout = 25 * time.Millisecond

	started := time.Now()
	_, err := adapter.SearchURL(context.Background(), provider.SearchRequest{
		URL:        "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-14",
		Passengers: provider.Passengers{Adults: 1},
	})
	if time.Since(started) > time.Second {
		t.Fatalf("page timeout took %v", time.Since(started))
	}
	var providerErr *provider.Error
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %v, want provider error", err)
	}
	if providerErr.Kind != provider.ErrTimeout || providerErr.Op != "mrbilit.search.page" || providerErr.StatusCode != 0 {
		t.Fatalf("error = %+v, want timeout with no upstream status", providerErr)
	}
}

func TestSearchURLPageHTTP504IsNotTimeout(t *testing.T) {
	page := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGatewayTimeout)
	})
	adapter := searchTestProvider(t, page, validSearchAPI(t, observedFixture(t)))

	_, err := adapter.SearchURL(context.Background(), provider.SearchRequest{
		URL:        "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-14",
		Passengers: provider.Passengers{Adults: 1},
	})
	var providerErr *provider.Error
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %v, want provider error", err)
	}
	if providerErr.Kind != provider.ErrHTTPStatus || providerErr.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("error = %+v, want upstream 504", providerErr)
	}
}

type pageReadErrorTransport struct{}

func (pageReadErrorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		Status:        "200 OK",
		StatusCode:    http.StatusOK,
		Header:        http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:          pageReadErrorBody{},
		ContentLength: -1,
	}, nil
}

type pageReadErrorBody struct{}

func (pageReadErrorBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (pageReadErrorBody) Close() error             { return nil }

type pageRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f pageRoundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type pageContextBody struct {
	ctx     context.Context
	started chan<- struct{}
}

func (b *pageContextBody) Read([]byte) (int, error) {
	select {
	case b.started <- struct{}{}:
	default:
	}
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *pageContextBody) Close() error { return nil }

func TestSearchURLPageBodyReadTimeout(t *testing.T) {
	headersFlushed := make(chan struct{})
	page := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("page response writer does not support flushing")
			return
		}
		flusher.Flush()
		close(headersFlushed)
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	})
	adapter := searchTestProvider(t, page, validSearchAPI(t, observedFixture(t)))
	adapter.pageClient.Timeout = 200 * time.Millisecond

	started := time.Now()
	_, err := adapter.SearchURL(context.Background(), provider.SearchRequest{
		URL:        "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-14",
		Passengers: provider.Passengers{Adults: 1},
	})
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("page body-read timeout took %v", elapsed)
	}
	select {
	case <-headersFlushed:
	case <-time.After(time.Second):
		t.Fatal("page headers were not flushed")
	}
	var providerErr *provider.Error
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %v, want provider error", err)
	}
	if providerErr.Kind != provider.ErrTimeout || providerErr.Op != "mrbilit.search.page.read" || providerErr.StatusCode != http.StatusOK {
		t.Fatalf("error = %+v, want body-read timeout with status 200", providerErr)
	}
}

func TestSearchURLPageBodyReadErrorIsNetwork(t *testing.T) {
	adapter := searchTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unexpected page request", http.StatusInternalServerError)
	}), validSearchAPI(t, observedFixture(t)))
	adapter.pageClient.Transport = pageReadErrorTransport{}

	_, err := adapter.SearchURL(context.Background(), provider.SearchRequest{
		URL:        "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-14",
		Passengers: provider.Passengers{Adults: 1},
	})
	var providerErr *provider.Error
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %v, want provider error", err)
	}
	if providerErr.Kind != provider.ErrNetwork || providerErr.Op != "mrbilit.search.page.read" || providerErr.StatusCode != http.StatusOK {
		t.Fatalf("error = %+v, want non-timeout body-read error with status 200", providerErr)
	}
}

func TestSearchURLPageCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 1)
	adapter := searchTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unexpected page request", http.StatusInternalServerError)
	}), validSearchAPI(t, observedFixture(t)))
	adapter.pageClient.Transport = pageRoundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			Status:        "200 OK",
			StatusCode:    http.StatusOK,
			Header:        http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body:          &pageContextBody{ctx: ctx, started: started},
			ContentLength: -1,
		}, nil
	})
	adapter.pageClient.Timeout = 5 * time.Second

	errCh := make(chan error, 1)
	go func() {
		_, err := adapter.SearchURL(ctx, provider.SearchRequest{
			URL:        "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-14",
			Passengers: provider.Passengers{Adults: 1},
		})
		errCh <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("page body read did not start")
	}
	cancel()
	select {
	case err := <-errCh:
		var providerErr *provider.Error
		if !errors.As(err, &providerErr) {
			t.Fatalf("error = %v, want provider error", err)
		}
		if providerErr.Kind != provider.ErrCanceled || providerErr.Op != "mrbilit.http" || providerErr.StatusCode != 0 {
			t.Fatalf("error = %+v, want caller cancellation", providerErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SearchURL did not return after cancellation")
	}
}

func TestSearchURLPageCallerDeadline(t *testing.T) {
	started := make(chan struct{}, 1)
	adapter := searchTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unexpected page request", http.StatusInternalServerError)
	}), validSearchAPI(t, observedFixture(t)))
	adapter.pageClient.Timeout = 5 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	adapter.pageClient.Transport = pageRoundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			Status:        "200 OK",
			StatusCode:    http.StatusOK,
			Header:        http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body:          &pageContextBody{ctx: ctx, started: started},
			ContentLength: -1,
		}, nil
	})

	errCh := make(chan error, 1)
	go func() {
		_, err := adapter.SearchURL(ctx, provider.SearchRequest{
			URL:        "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-14",
			Passengers: provider.Passengers{Adults: 1},
		})
		errCh <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("page body read did not start")
	}
	select {
	case err := <-errCh:
		var providerErr *provider.Error
		if !errors.As(err, &providerErr) {
			t.Fatalf("error = %v, want provider error", err)
		}
		if providerErr.Kind != provider.ErrTimeout || providerErr.Op != "mrbilit.http" || providerErr.StatusCode != 0 {
			t.Fatalf("error = %+v, want caller deadline timeout", providerErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SearchURL did not return after caller deadline")
	}
}

func TestSearchURLRouteMismatchIsInvalid(t *testing.T) {
	fixture := strings.Replace(string(observedFixture(t)), `"from": 219`, `"from": 220`, 1)
	adapter := searchTestProvider(t,
		validSearchPage(t),
		validSearchAPI(t, []byte(fixture)),
	)
	_, err := adapter.SearchURL(context.Background(), provider.SearchRequest{
		URL:        "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-14",
		Passengers: provider.Passengers{Adults: 1},
	})
	var providerErr *provider.Error
	if !errors.As(err, &providerErr) || providerErr.Kind != provider.ErrInvalidResponse {
		t.Fatalf("error = %v", err)
	}
}

func TestExtractRouteIDsConflictingPageData(t *testing.T) {
	page := `<script>window.__NUXT__={urlParams:{originCode:219,destinationCode:191},other:{urlParams:{originCode:220,destinationCode:192}}}</script>`
	_, _, err := extractRouteIDs([]byte(page))
	var providerErr *provider.Error
	if !errors.As(err, &providerErr) || providerErr.Kind != provider.ErrInvalidResponse {
		t.Fatalf("error = %v", err)
	}
}

func TestExtractRouteIDsStationFallback(t *testing.T) {
	page := `<script>domesticOrigin:{title:"یزد",stationId:219},domesticDestination:{title:"مشهد",stationId:191}</script>`
	origin, destination, err := extractRouteIDs([]byte(page))
	if err != nil {
		t.Fatalf("extractRouteIDs: %v", err)
	}
	if origin != "219" || destination != "191" {
		t.Fatalf("route = %s/%s", origin, destination)
	}
}

func TestSearchURLEmptyTrainsIsSuccessful(t *testing.T) {
	fixture := []byte(`{"fromLocation":{"title":"Yazd"},"toLocation":{"title":"Mashhad"},"meta":{"routeNotFound":false},"trains":[]}`)
	adapter := searchTestProvider(t,
		validSearchPage(t),
		validSearchAPI(t, fixture),
	)
	result, err := adapter.SearchURL(context.Background(), provider.SearchRequest{
		URL:        "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-14",
		Passengers: provider.Passengers{Adults: 1},
	})
	if err != nil {
		t.Fatalf("SearchURL: %v", err)
	}
	if len(result.Result.Journeys) != 0 {
		t.Fatalf("journeys = %+v", result.Result.Journeys)
	}
	if result.Route.OriginName != "Yazd" || result.Route.DestinationName != "Mashhad" {
		t.Fatalf("route names = %+v", result.Route)
	}
}

func TestDefaultSearchPageBaseIsPublicSite(t *testing.T) {
	adapter, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if adapter.pageBase == nil || adapter.pageBase.String() != "https://mrbilit.com" {
		t.Fatalf("page base = %v", adapter.pageBase)
	}
}
