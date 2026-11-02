#define _DEFAULT_SOURCE
#ifdef _WIN32
#include <curses.h>
#include <windows.h>
#else
#include <ncurses.h>
#include <unistd.h>
#endif

#include <stdlib.h>
#include <string.h>
#include <time.h>

#define MAX_WORDS 10
#define SPAWN_RATE 1000   // ms
#define INITIAL_SPEED 100 // Slower: ms per move
#define GRID_WIDTH 80

typedef struct {
  char text[32];
  int current_len; // Remaining length
  int x, y;
  int active;
} Word;

Word words[MAX_WORDS];
int score = 0;
int game_over = 0;

#define DICT_SIZE 50
const char *dictionary[DICT_SIZE] = {
    "hello",    "world",   "ncurses",  "typing",     "game",     "keyboard",
    "coding",   "linux",   "clig",     "speed",      "accuracy", "terminal",
    "buffer",   "matrix",  "source",   "binary",     "pointer",  "variable",
    "function", "compile", "header",   "system",     "process",  "thread",
    "memory",   "random",  "shuffle",  "dictionary", "falling",  "gravity",
    "score",    "combo",   "perfect",  "great",      "good",     "miss",
    "failed",   "results", "launcher", "metadata",   "unified",  "build",
    "project",  "awesome", "rhythm",   "mania",      "snake",    "tetris",
    "puzzle",   "blocks"};

int shuffled_indices[DICT_SIZE];
int dict_idx = 0;

void shuffle_dictionary() {
  for (int i = 0; i < DICT_SIZE; i++)
    shuffled_indices[i] = i;
  for (int i = DICT_SIZE - 1; i > 0; i--) {
    int j = rand() % (i + 1);
    int temp = shuffled_indices[i];
    shuffled_indices[i] = shuffled_indices[j];
    shuffled_indices[j] = temp;
  }
  dict_idx = 0;
}

void spawn_word(int term_w, int term_h) {
  (void)term_h;
  for (int i = 0; i < MAX_WORDS; i++) {
    if (!words[i].active) {
      if (dict_idx >= DICT_SIZE)
        shuffle_dictionary();
      const char *w = dictionary[shuffled_indices[dict_idx++]];
      strcpy(words[i].text, w);
      words[i].current_len = strlen(w);

      int grid_start = (term_w - GRID_WIDTH) / 2;
      words[i].x = grid_start + (rand() % (GRID_WIDTH - words[i].current_len));

      words[i].y = 1;
      words[i].active = 1;
      break;
    }
  }
}

// Cross-platform sleep (ms)
void sleep_ms(int ms) {
#ifdef _WIN32
  Sleep(ms);
#else
  usleep(ms * 1000);
#endif
}

// Cross-platform monotonic time (ms)
long get_time_ms() {
#ifdef _WIN32
  return (long)GetTickCount64();
#else
  struct timespec ts;
  clock_gettime(CLOCK_MONOTONIC, &ts);
  return ts.tv_sec * 1000 + ts.tv_nsec / 1000000;
#endif
}

int main() {
  initscr();
  noecho();
  curs_set(0);
  keypad(stdscr, TRUE);
  nodelay(stdscr, TRUE);
  srand(time(NULL));
  shuffle_dictionary();

  if (has_colors()) {
    start_color();
    init_pair(1, COLOR_GREEN, COLOR_BLACK);
    init_pair(2, COLOR_RED, COLOR_BLACK);
    init_pair(3, COLOR_CYAN, COLOR_BLACK); // For borders
  }

  int term_h, term_w;
  getmaxyx(stdscr, term_h, term_w);

  long last_spawn = 0;
  long last_move = 0;
  int move_speed = INITIAL_SPEED;

  while (!game_over) {
    getmaxyx(stdscr, term_h, term_w);
    long now = get_time_ms();

    if (now - last_spawn > SPAWN_RATE) {
      spawn_word(term_w, term_h);
      last_spawn = now;
    }

    if (now - last_move > move_speed) {
      for (int i = 0; i < MAX_WORDS; i++) {
        if (words[i].active) {
          words[i].y++;
          if (words[i].y >= term_h - 1)
            game_over = 1;
        }
      }
      last_move = now;
    }

    int ch = getch();
    if (ch == 'q' || ch == 'Q')
      break;

    if (ch != ERR) {
      int target_idx = -1;
      int max_y = -1;

      // Find the bottom-most active word (highest y)
      for (int i = 0; i < MAX_WORDS; i++) {
        if (words[i].active) {
          if (words[i].y > max_y) {
            max_y = words[i].y;
            target_idx = i;
          }
        }
      }

      if (target_idx != -1) {
        int i = target_idx;
        int original_len = strlen(words[i].text);
        int typed_idx = original_len - words[i].current_len;

        if (ch == words[i].text[typed_idx]) {
          words[i].current_len--;
          if (words[i].current_len <= 0) {
            words[i].active = 0;
            score += original_len * 10;
            if (move_speed > 50)
              move_speed--;
          }
        }
      }
    }

    clear();
    int grid_start = (term_w - GRID_WIDTH) / 2;

    // Background / UI
    attron(COLOR_PAIR(3));
    for (int y = 1; y < term_h; y++) {
      mvaddch(y, grid_start - 1, '|');
      mvaddch(y, grid_start + GRID_WIDTH, '|');
    }
    attroff(COLOR_PAIR(3));

    attron(A_BOLD);
    mvprintw(0, 2, "Type Game - Score: %d", score);
    mvhline(0, grid_start - 1, '=', GRID_WIDTH + 2);
    attroff(A_BOLD);

    for (int i = 0; i < MAX_WORDS; i++) {
      if (words[i].active) {
        int original_len = strlen(words[i].text);
        int typed_count = original_len - words[i].current_len;
        // Disappear from LEFT to RIGHT:
        // We draw starting from words[i].text[typed_count]
        // and we shift the X position by typed_count so it doesn't "jump" right
        mvprintw(words[i].y, words[i].x + typed_count, "%s",
                 &words[i].text[typed_count]);
      }
    }

    refresh();
    sleep_ms(10);
  }

  if (game_over) {
    attron(COLOR_PAIR(2) | A_BOLD);
    mvprintw(term_h / 2, (term_w - 10) / 2, "GAME OVER");
    mvprintw(term_h / 2 + 1, (term_w - 15) / 2, "Final Score: %d", score);
    attroff(COLOR_PAIR(2) | A_BOLD);
    refresh();
    nodelay(stdscr, FALSE);
    getch();
  }

  endwin();
  return 0;
}
