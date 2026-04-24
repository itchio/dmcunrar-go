# dmcunrar-go

[![Build Status](https://github.com/itchio/dmcunrar-go/actions/workflows/build.yml/badge.svg)](https://github.com/itchio/dmcunrar-go/actions/workflows/build.yml)
[![GoDoc](https://godoc.org/github.com/itchio/dmcunrar-go?status.svg)](https://godoc.org/github.com/itchio/dmcunrar-go)

Bindings to use `dmc_unrar` as a library from golang.

The vendored `dmc_unrar.c` is sourced from a hardened fork
(<https://github.com/leafo/dmc_unrar>) that adds cooperative cancellation,
resource caps, archive-bomb defenses, path-safety, and atomic extraction
on top of the upstream library.

### Links

- <https://github.com/leafo/dmc_unrar> — hardened fork used by this binding
- <https://github.com/DrMcCoy/dmc_unrar> — upstream

### License

dmcunrar-go is released under the GPL license, see the `LICENSE` file.
