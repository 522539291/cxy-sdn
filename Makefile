all: build test

build:
	        go build -v

test:
	        cd server && go test -covermode=count -test.short -coverprofile=coverage.out -v
	        cd util && go test -covermode=count -test.short -coverprofile=coverage.out -v
