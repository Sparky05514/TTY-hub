CC = gcc
CFLAGS = -Wall -Wextra -std=c11
LIBS = -lncurses

type: main.o
	$(CC) main.o -o type $(LIBS)

main.o: main.c
	$(CC) $(CFLAGS) -c main.c -o main.o

clean:
	rm -f type *.o

.PHONY: clean
