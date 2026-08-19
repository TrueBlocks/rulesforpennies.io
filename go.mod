module github.com/TrueBlocks/rulesforpennies.io

go 1.25.9

require (
	github.com/TrueBlocks/trueblocks-art/packages/ai v0.0.0-00010101000000-000000000000
	github.com/TrueBlocks/trueblocks-art/packages/appd v0.0.0-00010101000000-000000000000
	github.com/TrueBlocks/trueblocks-art/packages/creds v0.0.0
	modernc.org/sqlite v1.52.0
)

replace github.com/TrueBlocks/trueblocks-art/packages/ai => ../packages/ai

replace github.com/TrueBlocks/trueblocks-art/packages/creds => ../packages/creds

replace github.com/TrueBlocks/trueblocks-art/packages/appd => ../packages/appd

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/tools v0.45.0 // indirect
	modernc.org/libc v1.72.3 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)
