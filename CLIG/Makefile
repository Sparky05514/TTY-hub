SUBDIRS = snake tetris 2048 type

all: $(SUBDIRS) launcher

$(SUBDIRS):
	$(MAKE) -C $@

launcher: launcher.c
	gcc -Wall -Wextra launcher.c -o launcher -lncurses

clean:
	for dir in $(SUBDIRS); do \
		$(MAKE) -C $$dir clean; \
	done
	rm -f launcher

.PHONY: all clean $(SUBDIRS)
