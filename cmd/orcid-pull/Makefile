BIN := orcid-pull
DEPLOYTMPLDIR := ~/var/orcid
DEPLOYBINDIR := ~/bin

.PHONY: clean linux darwin

all: linux darwin

deploy: linux
	scp build/linux/$(BIN) ims.ut.ee:$(DEPLOYBINDIR) && scp orcid-list.tmpl ims.ut.ee:$(DEPLOYTMPLDIR) && scp publications-by-year.tmpl ims.ut.ee:$(DEPLOYTMPLDIR)

clean:
	rm -rf build/

linux:
	mkdir -p build/linux
	GOOS=linux GOARCH=amd64 go build -o build/linux/$(BIN)

darwin:
	mkdir -p build/linux
	GOOS=darwin GOARCH=amd64 go build -o build/darwin/$(BIN)
