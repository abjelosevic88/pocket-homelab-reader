module github.com/abjelosevic88/pocket-homelab-reader

go 1.26.5

require (
	github.com/abjelosevic88/pocket-homelab-core v0.18.5
	github.com/go-chi/chi/v5 v5.5.0
	golang.org/x/crypto v0.58.0
	modernc.org/sqlite v1.60.1
)

require (
	codeberg.org/readeck/go-readability/v2 v2.1.3 // indirect
	github.com/andybalholm/cascadia v1.3.3 // indirect
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-shiori/dom v0.0.0-20230515143342-73569d674e1c // indirect
	github.com/gogs/chardet v0.0.0-20211120154057-b7413eaefb8f // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/css v1.0.1 // indirect
	github.com/itlightning/dateparse v0.2.1 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/microcosm-cc/bluemonday v1.0.27 // indirect
	github.com/mmcdole/gofeed v1.5.0 // indirect
	github.com/mmcdole/goxpp/v2 v2.0.0 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/net v0.60.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	modernc.org/libc v1.77.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

// Reviewed SDK snapshot; see third_party/pocket-core/sdk.lock.json.
replace github.com/abjelosevic88/pocket-homelab-core => ./third_party/pocket-core
