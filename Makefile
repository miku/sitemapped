sitemapped: sitemapped.go
	go build $^

.PHONY: clean
clean:
	rm -rf sitemapped

