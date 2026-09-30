.PHONY: up down run test itest docker

up:            ## start postgres
	docker compose up -d --wait

down:          ## stop postgres and drop data
	docker compose down -v

run: up        ## run the API against compose postgres
	go run ./cmd/server

test:          ## unit tests only
	go test -short ./...

itest:         ## all tests incl. testcontainers integration
	go test -race ./...

docker:        ## build the production image
	docker build -t gobank .
