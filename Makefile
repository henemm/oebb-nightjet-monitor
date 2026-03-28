.PHONY: build run clean

build:
	go build -o oebb-nightjet-monitor .

run: build
	./oebb-nightjet-monitor -config config.yaml

once: build
	./oebb-nightjet-monitor -config config.yaml -once

clean:
	rm -f oebb-nightjet-monitor

docker-build:
	docker build -t oebb-nightjet-monitor .

docker-run:
	docker run --rm -v $(PWD)/config.yaml:/app/config.yaml oebb-nightjet-monitor
