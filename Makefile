MSG ?= update

.PHONY: build clean add commit push lint

build:

clean:

lint:

add:
	@git add -A

commit: lint
	@git add -A
	@git commit -m "$(MSG)" || true

push: lint
	@git add -A
	@git commit -m "$(MSG)" || true
	@git push
