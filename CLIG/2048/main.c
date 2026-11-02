#define _DEFAULT_SOURCE
#include <ncurses.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#define SIZE 4
#define WIN_VALUE 2048

int board[SIZE][SIZE];
int score = 0;
int max_score = 0;
int won = 0;
int game_over = 0;

void init_board() {
  memset(board, 0, sizeof(board));
  score = 0;
  won = 0;
  game_over = 0;
}

void add_random() {
  int empty[SIZE * SIZE][2];
  int count = 0;
  for (int i = 0; i < SIZE; i++) {
    for (int j = 0; j < SIZE; j++) {
      if (board[i][j] == 0) {
        empty[count][0] = i;
        empty[count][1] = j;
        count++;
      }
    }
  }
  if (count > 0) {
    int r = rand() % count;
    board[empty[r][0]][empty[r][1]] = (rand() % 10 == 0) ? 4 : 2;
  }
}

int can_move() {
  for (int i = 0; i < SIZE; i++) {
    for (int j = 0; j < SIZE; j++) {
      if (board[i][j] == 0)
        return 1;
      if (j < SIZE - 1 && board[i][j] == board[i][j + 1])
        return 1;
      if (i < SIZE - 1 && board[i][j] == board[i + 1][j])
        return 1;
    }
  }
  return 0;
}

int move_left() {
  int moved = 0;
  for (int i = 0; i < SIZE; i++) {
    int last_merge = -1;
    for (int j = 1; j < SIZE; j++) {
      if (board[i][j] != 0) {
        int k = j;
        while (k > 0 && board[i][k - 1] == 0) {
          board[i][k - 1] = board[i][k];
          board[i][k] = 0;
          k--;
          moved = 1;
        }
        if (k > 0 && board[i][k - 1] == board[i][k] && last_merge != k - 1) {
          board[i][k - 1] *= 2;
          score += board[i][k - 1];
          if (board[i][k - 1] == WIN_VALUE)
            won = 1;
          board[i][k] = 0;
          last_merge = k - 1;
          moved = 1;
        }
      }
    }
  }
  return moved;
}

void rotate() {
  int temp[SIZE][SIZE];
  for (int i = 0; i < SIZE; i++) {
    for (int j = 0; j < SIZE; j++) {
      temp[j][SIZE - 1 - i] = board[i][j];
    }
  }
  memcpy(board, temp, sizeof(board));
}

int game_move(int dir) {
  // 0: Left, 1: Up, 2: Right, 3: Down
  int moved = 0;
  for (int i = 0; i < dir; i++)
    rotate();
  moved = move_left();
  for (int i = 0; i < (4 - dir) % 4; i++)
    rotate();
  return moved;
}

void init_colors() {
  if (has_colors()) {
    start_color();
    // Pair: foreground, background
    init_pair(1, COLOR_BLACK, COLOR_WHITE);   // 2
    init_pair(2, COLOR_BLACK, COLOR_YELLOW);  // 4
    init_pair(3, COLOR_BLACK, COLOR_GREEN);   // 8
    init_pair(4, COLOR_BLACK, COLOR_CYAN);    // 16
    init_pair(5, COLOR_WHITE, COLOR_BLUE);    // 32
    init_pair(6, COLOR_WHITE, COLOR_MAGENTA); // 64
    init_pair(7, COLOR_WHITE, COLOR_RED);     // 128
    init_pair(8, COLOR_BLACK, COLOR_WHITE);   // 256 (reuse or custom)
    init_pair(9, COLOR_BLACK, COLOR_YELLOW);  // 512
    init_pair(10, COLOR_BLACK, COLOR_GREEN);  // 1024
    init_pair(11, COLOR_BLACK, COLOR_BLUE);   // 2048
  }
}

int get_color_pair(int value) {
  int log2 = 0;
  if (value == 0)
    return 0;
  while (value > 1) {
    value >>= 1;
    log2++;
  }
  return (log2 > 11) ? 11 : log2;
}

void draw() {
  clear();
  int term_h, term_w;
  getmaxyx(stdscr, term_h, term_w);

  int cell_w = 9;
  int cell_h = 4;
  int board_w = SIZE * cell_w + 1;
  int board_h = SIZE * cell_h + 1;

  int offset_y = (term_h - board_h) / 2;
  int offset_x = (term_w - board_w) / 2;

  attron(A_BOLD);
  mvprintw(offset_y - 2, offset_x + (board_w - 6) / 2, " 2048 ");
  attroff(A_BOLD);
  mvprintw(offset_y - 1, offset_x, "Score: %d", score);

  for (int i = 0; i < SIZE; i++) {
    for (int j = 0; j < SIZE; j++) {
      int y = offset_y + i * cell_h;
      int x = offset_x + j * cell_w;
      int pair = get_color_pair(board[i][j]);

      // Draw cell background and borders
      if (board[i][j] != 0)
        attron(COLOR_PAIR(pair));

      for (int r = 0; r <= cell_h; r++) {
        for (int c = 0; c <= cell_w; c++) {
          if (r == 0 || r == cell_h) {
            mvaddch(y + r, x + c, ACS_HLINE);
          } else if (c == 0 || c == cell_w) {
            mvaddch(y + r, x + c, ACS_VLINE);
          } else if (board[i][j] != 0) {
            mvaddch(y + r, x + c, ' ');
          }
        }
      }
      // Corners
      mvaddch(y, x, ACS_PLUS);
      mvaddch(y, x + cell_w, ACS_PLUS);
      mvaddch(y + cell_h, x, ACS_PLUS);
      mvaddch(y + cell_h, x + cell_w, ACS_PLUS);

      if (board[i][j] != 0) {
        char s[10];
        sprintf(s, "%d", board[i][j]);
        attron(A_BOLD);
        mvprintw(y + cell_h / 2, x + (cell_w - strlen(s)) / 2, "%s", s);
        attroff(A_BOLD);
        attroff(COLOR_PAIR(pair));
      }
    }
  }

  if (won) {
    mvprintw(offset_y + board_h + 1, offset_x,
             "YOU REACHED 2048! Press 'c' to continue.");
  }
  if (game_over) {
    attron(COLOR_PAIR(7) | A_BOLD);
    mvprintw(offset_y + board_h + 1, offset_x,
             "GAME OVER! Press 'r' to restart or 'q' to quit.");
    attroff(COLOR_PAIR(7) | A_BOLD);
  } else {
    mvprintw(offset_y + board_h + 1, offset_x,
             "Use Arrow Keys or WASD to move. 'q' to quit.");
  }

  refresh();
}

int main() {
  initscr();
  noecho();
  curs_set(FALSE);
  keypad(stdscr, TRUE);
  init_colors();
  srand(time(NULL));

  init_board();
  add_random();
  add_random();

  while (1) {
    draw();
    int ch = getch();

    if (ch == 'q' || ch == 'Q')
      break;
    if (game_over && (ch == 'r' || ch == 'R')) {
      init_board();
      add_random();
      add_random();
      continue;
    }

    int moved = 0;
    switch (ch) {
    case KEY_LEFT:
    case 'a':
    case 'A':
      moved = game_move(0);
      break;
    case KEY_UP:
    case 'w':
    case 'W':
      moved = game_move(3);
      break;
    case KEY_RIGHT:
    case 'd':
    case 'D':
      moved = game_move(2);
      break;
    case KEY_DOWN:
    case 's':
    case 'S':
      moved = game_move(1);
      break;
    }

    if (moved) {
      add_random();
      if (!can_move()) {
        game_over = 1;
      }
    }
  }

  endwin();
  return 0;
}
