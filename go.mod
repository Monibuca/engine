module github.com/Monibuca/engine/v3

go 1.13

require (
	github.com/BurntSushi/toml v0.3.1
	github.com/Monibuca/utils/v3 v3.0.0-alpha2
	github.com/logrusorgru/aurora v2.0.3+incompatible
	github.com/pkg/errors v0.9.1
)

replace github.com/Monibuca/utils/v3 v3.0.0-alpha2 => ../utils
