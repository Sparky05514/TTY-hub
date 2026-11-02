#define _DEFAULT_SOURCE
#include <ncurses.h>
#include <stdlib.h>
#include <time.h>
#include <unistd.h>
#include <sys/time.h>

/* Constants */
#define GAME_DELAY 69000 /* 60000 * 1.15 = 69000 (15% slower) */
#define FOOD_COUNT 10
#define QUEUE_SIZE 3

/* Structs */
typedef struct {
    int x; /* Logical X coordinate */
    int y; /* Logical Y coordinate */
} Point;

typedef struct {
    Point *body;
    int length;
    int dx;
    int dy;
} Snake;

/* Global Variables */
int logic_width, logic_height;
int score = 0;
int game_over = 0;
Point food[FOOD_COUNT];
Snake snake;

/* Input Queue */
int dir_queue[QUEUE_SIZE];
int queue_head = 0;
int queue_tail = 0;
int queue_count = 0;

/* Function Prototypes */
void init_ncurses();
void reset_game();
void queue_move(int dx, int dy);
void process_queue();
void logic();
void draw();
void cleanup();
int show_game_over();
long get_current_time_us();

int main() {
    init_ncurses();

    while (1) {
        reset_game();
        long last_update_time = get_current_time_us();

        while (!game_over) {
            long current_time = get_current_time_us();

            /* Input Polling Loop */
            int ch;
            while ((ch = getch()) != ERR) {
                 switch (ch) {
                    case KEY_LEFT:
                    case 'a':
                    case 'A':
                        queue_move(-1, 0); 
                        break;
                    case KEY_RIGHT: 
                    case 'd':
                    case 'D':
                        queue_move(1, 0); 
                        break;
                    case KEY_UP:    
                    case 'w':
                    case 'W':
                        queue_move(0, -1); 
                        break;
                    case KEY_DOWN:  
                    case 's':
                    case 'S':
                        queue_move(0, 1); 
                        break;
                    case 'q':
                    case 'Q':
                        game_over = 1;
                        break;
                }
            }

            if (current_time - last_update_time >= GAME_DELAY) {
                process_queue();
                logic();
                draw();
                last_update_time = current_time;
            }

            usleep(1000); 
        }

        if (!show_game_over()) {
            break;
        }
    }

    cleanup();
    return 0;
}

long get_current_time_us() {
    struct timeval tv;
    gettimeofday(&tv, NULL);
    return (tv.tv_sec * 1000000) + tv.tv_usec;
}

void init_ncurses() {
    initscr();
    noecho();
    curs_set(FALSE);
    keypad(stdscr, TRUE);
    nodelay(stdscr, TRUE); /* Non-blocking getch */
    srand(time(NULL));

    if (has_colors()) {
        start_color();
        init_pair(1, COLOR_BLACK, COLOR_GREEN); /* Snake body */
        init_pair(2, COLOR_BLACK, COLOR_RED);   /* Food */
    }

    /* Screen dimensions */
    int term_x, term_y;
    getmaxyx(stdscr, term_y, term_x);
    
    logic_width = term_x / 2;
    logic_height = term_y;
}

void reset_game() {
    if (snake.body) {
        free(snake.body);
    }

    score = 0;
    game_over = 0;
    queue_head = 0;
    queue_tail = 0;
    queue_count = 0;

    /* Initialize Snake */
    snake.length = 5;
    snake.body = malloc(sizeof(Point) * (logic_width * logic_height));
    snake.body[0].x = logic_width / 2;
    snake.body[0].y = logic_height / 2;
    snake.dx = 1; /* Starting moving RIGHT */
    snake.dy = 0;

    /* Initialize Food */
    for (int i = 0; i < FOOD_COUNT; i++) {
        food[i].x = rand() % (logic_width - 2) + 1;
        food[i].y = rand() % (logic_height - 2) + 1;
    }
}

int show_game_over() {
    nodelay(stdscr, FALSE); // Blocking input for menu
    
    int h = 10, w = 40;
    int y = (LINES - h) / 2;
    int x = (COLS - w) / 2;
    
    WINDOW *win = newwin(h, w, y, x);
    box(win, 0, 0);
    mvwprintw(win, 2, (w - 11) / 2, "GAME OVER");
    mvwprintw(win, 4, (w - 16) / 2, "Final Score: %d", score);
    mvwprintw(win, 6, (w - 24) / 2, "Press 'r' to Restart");
    mvwprintw(win, 7, (w - 20) / 2, "Press 'q' to Quit");
    wrefresh(win);
    
    int ch;
    while (1) {
        ch = getch();
        if (ch == 'r' || ch == 'R') {
            nodelay(stdscr, TRUE); // Restore non-blocking
            delwin(win);
            return 1;
        }
        if (ch == 'q' || ch == 'Q') {
            nodelay(stdscr, TRUE); // Restore non-blocking
            delwin(win);
            return 0;
        }
    }
}

void queue_move(int dx, int dy) {
    if (queue_count >= QUEUE_SIZE) return;

    /* Determine the "current" direction to check against (last queued or actual snake dir) */
    int last_dx, last_dy;
    if (queue_count > 0) {
        int last_idx = (queue_tail - 1 + QUEUE_SIZE) % QUEUE_SIZE;
        /* Decode the queued integer into dx/dy. 0=Left, 1=Right, 2=Up, 3=Down */
        int val = dir_queue[last_idx];
        if (val == 0) { last_dx = -1; last_dy = 0; }
        else if (val == 1) { last_dx = 1; last_dy = 0; }
        else if (val == 2) { last_dx = 0; last_dy = -1; }
        else { last_dx = 0; last_dy = 1; }
    } else {
        last_dx = snake.dx;
        last_dy = snake.dy;
    }

    /* Prevent 180 degree turns */
    if (dx == -last_dx && dy == -last_dy) return;
    /* Prevent redundant same-direction moves outputting to queue (optional but saves space) */
    if (dx == last_dx && dy == last_dy) return;

    /* Encode dx/dy to simple int for queue */
    int val = 0;
    if (dx == -1) val = 0;
    else if (dx == 1) val = 1;
    else if (dy == -1) val = 2;
    else if (dy == 1) val = 3;

    dir_queue[queue_tail] = val;
    queue_tail = (queue_tail + 1) % QUEUE_SIZE;
    queue_count++;
}

void process_queue() {
    if (queue_count > 0) {
        int val = dir_queue[queue_head];
        queue_head = (queue_head + 1) % QUEUE_SIZE;
        queue_count--;

        if (val == 0) { snake.dx = -1; snake.dy = 0; }
        else if (val == 1) { snake.dx = 1; snake.dy = 0; }
        else if (val == 2) { snake.dx = 0; snake.dy = -1; }
        else if (val == 3) { snake.dx = 0; snake.dy = 1; }
    }
}

void logic() {
    Point next_head = {snake.body[0].x + snake.dx, snake.body[0].y + snake.dy};

    /* Wall Wrapping (Logical Coordinates) */
    if (next_head.x >= logic_width) next_head.x = 0;
    else if (next_head.x < 0) next_head.x = logic_width - 1;
    
    if (next_head.y >= logic_height) next_head.y = 0;
    else if (next_head.y < 0) next_head.y = logic_height - 1;

    /* Self Collision */
    for (int i = 0; i < snake.length; i++) {
        if (next_head.x == snake.body[i].x && next_head.y == snake.body[i].y) {
            game_over = 1;
            return;
        }
    }

    /* Move Snake Body */
    for (int i = snake.length; i > 0; i--) {
        snake.body[i] = snake.body[i - 1];
    }
    snake.body[0] = next_head;

    /* Eat Food */
    for (int i = 0; i < FOOD_COUNT; i++) {
        if (next_head.x == food[i].x && next_head.y == food[i].y) {
            score += 10;
            snake.length++;
            /* Spawn new food */
            food[i].x = rand() % (logic_width - 2) + 1;
            food[i].y = rand() % (logic_height - 2) + 1;
            break; 
        }
    }
}

void draw() {
    clear(); 

    /* Draw Food */
    if (has_colors()) attron(COLOR_PAIR(2));
    for (int i = 0; i < FOOD_COUNT; i++) {
        mvprintw(food[i].y, food[i].x * 2, "  ");
    }
    if (has_colors()) attroff(COLOR_PAIR(2));

    /* Draw Snake */
    if (has_colors()) attron(COLOR_PAIR(1));
    for (int i = 0; i < snake.length; i++) {
        mvprintw(snake.body[i].y, snake.body[i].x * 2, "  ");
    }
    if (has_colors()) attroff(COLOR_PAIR(1));

    /* Draw Score */
    attrset(A_NORMAL);
    mvprintw(0, 0, "Score: %d", score);

    refresh();
}

void cleanup() {
    free(snake.body);
    endwin();
}
