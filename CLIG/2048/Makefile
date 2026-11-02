CC = gcc
CFLAGS = -Wall -Wextra -std=c11
LDFLAGS = -lncurses

all: 2048

2048: main.c
	$(CC) $(CFLAGS) main.c -o 2048 $(LDFLAGS)

clean:
	rm -f 2048

.PHONY: all clean
