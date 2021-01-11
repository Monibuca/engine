module github.com/Monibuca/engine/v3

go 1.13

require (
	github.com/BurntSushi/toml v0.3.1
	github.com/funny/slab v0.0.0-20180511031532-b1fad5e5d478
	github.com/logrusorgru/aurora v0.0.0-20200102142835-e9ef32dff381
	github.com/mattn/go-colorable v0.1.6
	github.com/pkg/errors v0.9.1
	github.com/pion/rtp v1.5.4
	github.com/Monibuca/utils/v3 v3.0.0-alpha2
)

replace github.com/Monibuca/utils/v3 v3.0.0-alpha2 => ../../utils/v3