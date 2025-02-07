GO := go
GO_BUILD = CGO_ENABLED=0 $(GO) build
GO_GENERATE = $(GO) generate
GO_TAGS ?= bpf
TARGET = blxrep-bpf
BINDIR ?= /usr/local/bin
VERSION=$(shell git describe --tags --always)

# BPF-specific variables
CLANG ?= clang
CFLAGS := -O2 -g -Wall -Werror
BPF_CFLAGS := $(CFLAGS) \
	-target bpf \
	-D__TARGET_ARCH_x86

.PHONY: all clean generate $(TARGET)

all: generate $(TARGET)

$(TARGET):
	$(GO_GENERATE)
	$(GO_BUILD) $(if $(GO_TAGS),-tags $(GO_TAGS)) \
		-ldflags "-w -s \
		-X 'github.com/xmigrate/blxrep.Version=${VERSION}'"

# Generate BPF code from .c to .o
%.bpf.o: %.bpf.c
	$(CLANG) $(BPF_CFLAGS) -c $< -o $@

# Generate Go files from BPF objects
generate: export BPF_CLANG := $(CLANG)
generate: export BPF_CFLAGS := $(BPF_CFLAGS)
generate:
	$(GO_GENERATE)

clean:
	rm -f $(TARGET)
	rm -f *.o
	rm -f *.bpf.o
	rm -f *_bpf_*.go
	rm -rf ./release