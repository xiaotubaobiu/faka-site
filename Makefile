.PHONY: css build run dev clean
css:
	npx tailwindcss -i src/input.css -o internal/web/static/app.css --minify
build: css
	go build -o faka-site .
run: build
	FAKA_DB=./data/faka.db FAKA_LISTEN=:8090 SESSION_SECRET=devsecret COOKIE_SECURE=false ./faka-site
dev: css
	go run .
clean:
	rm -f faka-site
