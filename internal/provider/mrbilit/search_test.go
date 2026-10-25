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
