httpts:main.go
	go build -gcflags "-N -l" -o httpts main.go
clean:
	rm -rf httpts
