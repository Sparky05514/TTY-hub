#define _DEFAULT_SOURCE
#include <ncurses.h>
#include <stdlib.h>
#include <time.h>
#include <unistd.h>
#include <string.h>
#include <sys/time.h>

/* Constants */
#define DELAY 5000          /* 5ms tick rate (approx 200 FPS for input) */
#define DROP_RATE_INITIAL 500000 
#define LOCK_DELAY 500000   /* 0.5s lock delay */

#define BOARD_WIDTH 10
#define BOARD_HEIGHT 20

/* Structs */
typedef struct {
    int shape[4][4];
    int color;
    int grid_size; /* 2, 3, or 4 */
} TetrominoDef;

typedef struct {
    int x;
    int y;
    int type;      
    int rotation;  /* 0..3 */
    int color;
} Tetromino;

/* Global Variables */
int board[BOARD_HEIGHT][BOARD_WIDTH] = {0};
Tetromino current_piece;
int game_over = 0;
int score = 0;
long last_drop_time = 0;
long drop_rate = DROP_RATE_INITIAL;
long lock_timer = 0;

/* Advanced Mechanics State */
/* Advanced Mechanics State */
int next_piece_type;
int hold_piece_type = -1; /* -1 means empty */
int can_hold = 1;

/* Shapes Definition */
const TetrominoDef SHAPES[7] = {
    /* I - Type 0 */
    { { {0,0,0,0}, {1,1,1,1}, {0,0,0,0}, {0,0,0,0} }, 1, 4 },
    /* J - Type 1 */
    { { {1,0,0,0}, {1,1,1,0}, {0,0,0,0}, {0,0,0,0} }, 2, 3 },
    /* L - Type 2 */
    { { {0,0,1,0}, {1,1,1,0}, {0,0,0,0}, {0,0,0,0} }, 3, 3 },
    /* O - Type 3 */
    { { {1,1,0,0}, {1,1,0,0}, {0,0,0,0}, {0,0,0,0} }, 4, 2 },
    /* S - Type 4 */
    { { {0,1,1,0}, {1,1,0,0}, {0,0,0,0}, {0,0,0,0} }, 5, 3 },
    /* T - Type 5 */
    { { {0,1,0,0}, {1,1,1,0}, {0,0,0,0}, {0,0,0,0} }, 6, 3 },
    /* Z - Type 6 */
    { { {1,1,0,0}, {0,1,1,0}, {0,0,0,0}, {0,0,0,0} }, 7, 3 }
};

/* --- LOGIC DEFINITIONS START --- */

/* Randomizer State */
int bag[7];
int bag_ptr = 7;

void shuffle_bag() {
    for (int i = 0; i < 7; i++) bag[i] = i;
    for (int i = 6; i > 0; i--) {
        int j = rand() % (i + 1);
        int temp = bag[i];
        bag[i] = bag[j];
        bag[j] = temp;
    }
    bag_ptr = 0;
}

int next_from_bag() {
    if (bag_ptr >= 7) shuffle_bag();
    return bag[bag_ptr++];
}

/* SRS Wall Kick Data (J, L, S, T, Z) - Adapted for Y-Down (Screen) Coords */
const int KICKS_JLSTZ[8][5][2] = {
    {{0,0}, {-1,0}, {-1,-1}, { 0, 2}, {-1, 2}}, /* 0->1 */
    {{0,0}, { 1,0}, { 1, 1}, { 0,-2}, { 1,-2}}, /* 1->0 */
    {{0,0}, { 1,0}, { 1, 1}, { 0,-2}, { 1,-2}}, /* 1->2 */
    {{0,0}, {-1,0}, {-1,-1}, { 0, 2}, {-1, 2}}, /* 2->1 */
    {{0,0}, { 1,0}, { 1,-1}, { 0, 2}, { 1, 2}}, /* 2->3 */
    {{0,0}, {-1,0}, {-1, 1}, { 0,-2}, {-1,-2}}, /* 3->2 */
    {{0,0}, {-1,0}, {-1, 1}, { 0,-2}, {-1,-2}}, /* 3->0 */
    {{0,0}, { 1,0}, { 1,-1}, { 0, 2}, { 1, 2}}  /* 0->3 */
};

const int KICKS_I[8][5][2] = {
    {{0,0}, {-2,0}, { 1,0}, {-2, 1}, { 1,-2}}, /* 0->1 */
    {{0,0}, { 2,0}, {-1,0}, { 2,-1}, {-1, 2}}, /* 1->0 */
    {{0,0}, {-1,0}, { 2,0}, {-1,-2}, { 2, 1}}, /* 1->2 */
    {{0,0}, { 1,0}, {-2,0}, { 1, 2}, {-2,-1}}, /* 2->1 */
    {{0,0}, { 2,0}, {-1,0}, { 2,-1}, {-1, 2}}, /* 2->3 */
    {{0,0}, {-2,0}, { 1,0}, {-2, 1}, { 1,-2}}, /* 3->2 */
    {{0,0}, { 1,0}, {-2,0}, { 1, 2}, {-2,-1}}, /* 3->0 */
    {{0,0}, {-1,0}, { 2,0}, {-1,-2}, { 2, 1}}  /* 0->3 */
};

int get_kick_index(int old_rot, int new_rot) {
    if (old_rot == 0 && new_rot == 1) return 0;
    if (old_rot == 1 && new_rot == 0) return 1;
    if (old_rot == 1 && new_rot == 2) return 2;
    if (old_rot == 2 && new_rot == 1) return 3;
    if (old_rot == 2 && new_rot == 3) return 4;
    if (old_rot == 3 && new_rot == 2) return 5;
    if (old_rot == 3 && new_rot == 0) return 6;
    if (old_rot == 0 && new_rot == 3) return 7;
    return 0;
}
/* --- LOGIC DEFINITIONS END --- */

/* Prototypes */
void init_ncurses();
void reset_game();
int show_game_over();
void loop_game();
void draw_board();
void spawn_piece(int type);
void new_piece();
int check_collision(int px, int py, int prot);
void rotate_piece(int dir);
void move_piece(int dx, int dy);
void hard_drop();
void hold_piece();
void lock_piece();
void clear_lines();
long get_time_us();
int get_block(int type, int rot, int x, int y);

int main() {
    init_ncurses();
    
    while (1) {
        reset_game();
        loop_game();
        if (!show_game_over()) break;
    }

    endwin();
    return 0;
}

long get_time_us() {
    struct timeval tv;
    gettimeofday(&tv, NULL);
    return (tv.tv_sec * 1000000) + tv.tv_usec;
}

void init_ncurses() {
    initscr();
    noecho();
    curs_set(FALSE);
    keypad(stdscr, TRUE);
    nodelay(stdscr, TRUE);
    srand(time(NULL));

    if (has_colors()) {
        start_color();
        init_pair(1, COLOR_BLACK, COLOR_CYAN);
        init_pair(2, COLOR_BLACK, COLOR_BLUE);
        init_pair(3, COLOR_BLACK, COLOR_YELLOW); 
        init_pair(4, COLOR_BLACK, COLOR_YELLOW);
        init_pair(5, COLOR_BLACK, COLOR_GREEN);
        init_pair(6, COLOR_BLACK, COLOR_MAGENTA);
        init_pair(7, COLOR_BLACK, COLOR_RED);
        init_pair(8, COLOR_WHITE, COLOR_BLACK);
    }
}

void reset_game() {
    memset(board, 0, sizeof(board));
    score = 0;
    game_over = 0;
    drop_rate = DROP_RATE_INITIAL;
    hold_piece_type = -1;
    can_hold = 1;
    lock_timer = 0;
    
    shuffle_bag();
    next_piece_type = next_from_bag();
    new_piece();
    last_drop_time = get_time_us();
}

int show_game_over() {
    nodelay(stdscr, FALSE); // Blocking input
    
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
            nodelay(stdscr, TRUE);
            delwin(win);
            return 1;
        }
        if (ch == 'q' || ch == 'Q') {
            nodelay(stdscr, TRUE);
            delwin(win);
            return 0;
        }
    }
}

void spawn_piece(int type) {
    current_piece.type = type;
    current_piece.x = (BOARD_WIDTH - SHAPES[type].grid_size) / 2;
    current_piece.y = 0;
    current_piece.rotation = 0;
    current_piece.color = SHAPES[type].color;
    
    if (check_collision(current_piece.x, current_piece.y, current_piece.rotation)) {
        game_over = 1;
    }
    
    lock_timer = 0; /* Reset lock timer on spawn */
}

void new_piece() {
    spawn_piece(next_piece_type);
    next_piece_type = next_from_bag();
    can_hold = 1;
}

void hold_piece() {
    if (!can_hold) return;
    
    if (hold_piece_type == -1) {
        hold_piece_type = current_piece.type;
        new_piece();
    } else {
        int temp = hold_piece_type;
        hold_piece_type = current_piece.type;
        spawn_piece(temp);
    }
    
    can_hold = 0;
}

int get_block(int type, int rot, int x, int y) {
    int size = SHAPES[type].grid_size;
    if (size == 2) return SHAPES[type].shape[y][x]; 
    
    int r = rot % 4;
    int tx = x, ty = y;
    
    for (int i = 0; i < r; i++) {
        int temp = tx;
        tx = size - 1 - ty;
        ty = temp;
    }
    
    if (tx < 0 || tx >= 4 || ty < 0 || ty >= 4) return 0;

    return SHAPES[type].shape[ty][tx];
}

int check_collision(int px, int py, int prot) {
    int size = SHAPES[current_piece.type].grid_size;
    for (int i = 0; i < size; i++) {
        for (int j = 0; j < size; j++) {
            if (get_block(current_piece.type, prot, j, i)) {
                int bx = px + j;
                int by = py + i;
                
                if (bx < 0 || bx >= BOARD_WIDTH || by >= BOARD_HEIGHT) return 1;
                if (by >= 0 && board[by][bx]) return 1;
            }
        }
    }
    return 0;
}

void lock_piece() {
    int size = SHAPES[current_piece.type].grid_size;
    for (int i = 0; i < size; i++) {
        for (int j = 0; j < size; j++) {
            if (get_block(current_piece.type, current_piece.rotation, j, i)) {
                int by = current_piece.y + i;
                int bx = current_piece.x + j;
                if (by >= 0 && by < BOARD_HEIGHT && bx >= 0 && bx < BOARD_WIDTH) {
                    board[by][bx] = current_piece.color;
                }
            }
        }
    }
    clear_lines();
    new_piece();
}

void clear_lines() {
    int lines_cleared = 0;
    for (int y = BOARD_HEIGHT - 1; y >= 0; y--) {
        int full = 1;
        for (int x = 0; x < BOARD_WIDTH; x++) {
            if (!board[y][x]) { full = 0; break; }
        }
        if (full) {
            lines_cleared++;
            for (int k = y; k > 0; k--) {
                for (int x = 0; x < BOARD_WIDTH; x++) board[k][x] = board[k-1][x];
            }
            for (int x = 0; x < BOARD_WIDTH; x++) board[0][x] = 0;
            y++;
        }
    }
    if (lines_cleared > 0) {
        /* Award points based on lines cleared */
        switch (lines_cleared) {
            case 1: score += 100; break;
            case 2: score += 300; break;
            case 3: score += 500; break;
            case 4: score += 800; break;
        }
        
        /* Check for all-clear (perfect clear) bonus */
        int board_empty = 1;
        for (int y = 0; y < BOARD_HEIGHT && board_empty; y++) {
            for (int x = 0; x < BOARD_WIDTH; x++) {
                if (board[y][x]) {
                    board_empty = 0;
                    break;
                }
            }
        }
        if (board_empty) {
            score += 3000; /* All-clear bonus! */
        }
        
        if (drop_rate > 100000) drop_rate -= 10000;
    }
}

void reset_lock_timer_if_grounded(long now) {
    if (check_collision(current_piece.x, current_piece.y + 1, current_piece.rotation)) {
        lock_timer = now; /* Move reset */
    }
}

void move_piece(int dx, int dy) {
    if (!check_collision(current_piece.x + dx, current_piece.y + dy, current_piece.rotation)) {
        current_piece.x += dx;
        current_piece.y += dy;
        /* Logic: If we moved successfully, and we are ON GROUND, reset timer */
        /* Exception: Soft drop (dy=1). Usually soft drop DOES NOT reset timer endlessly, 
           but for simplicity here we might allow it or just ignore. 
           Let's apply reset only for lateral moves (dx!=0) to prevent stalling by dropping?
           Standard Tetris rules say ANY successful movement resets step counter (limited times).
           We will impl infinite reset for simplicity.
        */
        if (dx != 0) {
           reset_lock_timer_if_grounded(get_time_us()); 
        }
    }
}

void rotate_piece(int dir) {
    int old_rot = current_piece.rotation;
    int new_rot = (old_rot + 4 + dir) % 4;
    int kick_idx = get_kick_index(old_rot, new_rot);
    int (*kicks)[2] = NULL;
    
    /* Type 0 = I, Type 3 = O, Others = JLSTZ */
    if (current_piece.type == 0) {
        kicks = (int (*)[2])KICKS_I[kick_idx];
    } else if (current_piece.type == 3) {
        /* O piece rotates but no kicks */
        kicks = NULL;
    } else {
        kicks = (int (*)[2])KICKS_JLSTZ[kick_idx];
    }
    
    int rotated = 0;
    
    /* Basic Rotation + 5 SRS Tests */
    for (int i = 0; i < 5; i++) {
        int dx = 0, dy = 0;
        if (kicks) {
            dx = kicks[i][0];
            dy = kicks[i][1];
        } else {
            /* If no kicks (O piece), only test 0 */
            if (i > 0) break;
        }
        
        if (!check_collision(current_piece.x + dx, current_piece.y + dy, new_rot)) {
            current_piece.x += dx;
            current_piece.y += dy;
            current_piece.rotation = new_rot;
            rotated = 1;
            break;
        }
    }
    
    if (rotated) {
        reset_lock_timer_if_grounded(get_time_us());
    }
}

void hard_drop() {
    while (!check_collision(current_piece.x, current_piece.y + 1, current_piece.rotation)) {
        current_piece.y++;
    }
    lock_piece();
    last_drop_time = get_time_us();
}

void draw_preview(int start_y, int start_x, int type, const char* label) {
    mvprintw(start_y, start_x, "%s", label);
    
    /* Always clear/draw 4x4 area to ensure previous piece is erased */
    for(int i=0; i<4; i++) {
        for(int j=0; j<4; j++) {
            /* If type is -1 (empty), just clear the space */
            int is_block = 0;
            if (type != -1) {
                attron(COLOR_PAIR(SHAPES[type].color));
                is_block = get_block(type, 0, j, i);
            }
            
            if (is_block) {
                mvprintw(start_y + 2 + i, start_x + (j * 2), "  ");
            } else {
                /* Clear the space if it was previously occupied or is empty */
                /* Use default color pair or turn off attributes */
                attroff(COLOR_PAIR(1)|COLOR_PAIR(2)|COLOR_PAIR(3)|COLOR_PAIR(4)|COLOR_PAIR(5)|COLOR_PAIR(6)|COLOR_PAIR(7)); 
                /* Accessing global pairs blindly is risky if not careful, but safely we can just use stdscr default */
                /* Safer: turn off the specific color if we set it, but we can just use 0 */
                attrset(A_NORMAL);
                mvprintw(start_y + 2 + i, start_x + (j * 2), "  ");
            }
            
            if (type != -1) attroff(COLOR_PAIR(SHAPES[type].color));
        }
    }
}

void draw_board() {
    /* Remove global clear() to prevent flickering. 
       Only clear if terminal size changes. */
    static int prev_term_w = -1, prev_term_h = -1;
    int term_h, term_w;
    getmaxyx(stdscr, term_h, term_w);
    
    if (term_w != prev_term_w || term_h != prev_term_h) {
        prev_term_w = term_w;
        prev_term_h = term_h;
        clear();
    }
    
    int start_y = (term_h - BOARD_HEIGHT) / 2;
    int start_x = (term_w - (BOARD_WIDTH * 2)) / 2;

    /* Panels */
    draw_preview(start_y, start_x - 12, hold_piece_type, "HOLD");
    draw_preview(start_y, start_x + (BOARD_WIDTH * 2) + 4, next_piece_type, "NEXT");

    /* Frame */
    attron(COLOR_PAIR(8));
    for (int y = -1; y <= BOARD_HEIGHT; y++) {
        mvprintw(start_y + y, start_x - 2, "<!");
        mvprintw(start_y + y, start_x + (BOARD_WIDTH * 2), "!>");
    }
    for (int x = 0; x < BOARD_WIDTH * 2; x+=2) mvprintw(start_y + BOARD_HEIGHT, start_x + x, "==");
    attroff(COLOR_PAIR(8));

    /* Board */
    for (int y = 0; y < BOARD_HEIGHT; y++) {
        for (int x = 0; x < BOARD_WIDTH; x++) {
            if (board[y][x]) {
                attron(COLOR_PAIR(board[y][x]));
                mvprintw(start_y + y, start_x + (x * 2), "  ");
                attroff(COLOR_PAIR(board[y][x]));
            } else {
                /* Explicitly draw background to erase previous active pieces */
                attrset(A_NORMAL);
                mvprintw(start_y + y, start_x + (x * 2), " .");
            }
        }
    }

    /* Ghost Piece */
    int ghost_y = current_piece.y;
    while (!check_collision(current_piece.x, ghost_y + 1, current_piece.rotation)) {
        ghost_y++;
    }
    
    /* Draw Ghost */
    attron(COLOR_PAIR(current_piece.color) | A_DIM);
    for (int i = 0; i < 4; i++) {
        for (int j = 0; j < 4; j++) {
            if (get_block(current_piece.type, current_piece.rotation, j, i)) {
                int draw_y = start_y + ghost_y + i;
                int draw_x = start_x + (current_piece.x + j) * 2;
                if (draw_y >= start_y && draw_y < start_y + BOARD_HEIGHT) {
                    mvprintw(draw_y, draw_x, "::"); 
                }
            }
        }
    }
    attroff(COLOR_PAIR(current_piece.color) | A_DIM);

    /* Draw Active Piece */
    attron(COLOR_PAIR(current_piece.color));
    for (int i = 0; i < 4; i++) {
        for (int j = 0; j < 4; j++) {
            if (get_block(current_piece.type, current_piece.rotation, j, i)) {
                int draw_y = start_y + current_piece.y + i;
                int draw_x = start_x + (current_piece.x + j) * 2;
                if (draw_y >= start_y && draw_y < start_y + BOARD_HEIGHT) {
                    mvprintw(draw_y, draw_x, "  ");
                }
            }
        }
    }
    attroff(COLOR_PAIR(current_piece.color));

    attrset(A_NORMAL);
    mvprintw(0, 0, "Score: %d          ", score); /* Padding to clear long scores */
    refresh();
}

#define INPUT_KEEPALIVE 70000  /* 70ms: Strict for clear release detection */
#define ARR_DELAY 45000        /* 45ms: Slower speed (User req "too fast") */
#define DAS_DELAY 90000        /* 90ms: Snappy start (User req "too long") */
#define SDF_DELAY 70000        /* 70ms: ~15Hz Standard Soft Drop (Controllable) */

/* Input State */
long t_last_left = 0, t_last_right = 0, t_last_down = 0;
long acc_left = 0, acc_right = 0, acc_down = 0;



void loop_game() {
    while (!game_over) {
        long now = get_time_us();
        
        /* Input Handling - Process all pending keys */
        int ch;
        while ((ch = getch()) != ERR) {
            switch (ch) {
                /* Handled by Sticky Logic */
                case 'a':
                case 'A': 
                case KEY_LEFT:
                    if (now - t_last_left > INPUT_KEEPALIVE) {
                        /* New Press or Resume */
                        move_piece(-1, 0); 
                        
                        /* DAS Preservation: If gap is short and we were already speeding, don't reset */
                        if (now - t_last_left < 300000 && acc_left > 0) {
                            /* Keep momentum */
                        } else {
                            acc_left = -DAS_DELAY;
                        }
                    }
                    t_last_left = now; 
                    break;
                    
                case 'd':
                case 'D': 
                case KEY_RIGHT:
                    if (now - t_last_right > INPUT_KEEPALIVE) {
                        /* New Press or Resume */
                        move_piece(1, 0); 
                        
                        /* DAS Preservation: If gap is short and we were already speeding, don't reset */
                        if (now - t_last_right < 300000 && acc_right > 0) {
                             /* Keep momentum */
                        } else {
                            acc_right = -DAS_DELAY;
                        }
                    }
                    t_last_right = now; 
                    break;
                    

                case 's':
                case 'S': 
                case KEY_DOWN:
                    if (now - t_last_down > INPUT_KEEPALIVE) {
                        /* New Press */
                        move_piece(0, 1);
                        /* acc_down = 0 ? Actually we want it to start dropping if held */
                        acc_down = 0;
                    }
                    t_last_down = now; 
                    break;

                /* Single Action Keys */
                case 'j':
                case 'J': rotate_piece(-1); break;
                case 'k':
                case 'K': rotate_piece(1); break;
                case ' ': hard_drop(); break;
                case 'c':
                case 'C':
                case 'h':
                case 'H': hold_piece(); break;
                case KEY_UP: rotate_piece(1); break; 
                case 'q': game_over = 1; break;
            }
        }

        /* Sticky / ARR Logic */
        /* Left */
        if (now - t_last_left < INPUT_KEEPALIVE) {
            acc_left += DELAY;
            while (acc_left >= ARR_DELAY) {
                move_piece(-1, 0);
                acc_left -= ARR_DELAY;
            }
        }
        /* Right */
        if (now - t_last_right < INPUT_KEEPALIVE) {
            acc_right += DELAY;
            while (acc_right >= ARR_DELAY) {
                move_piece(1, 0);
                acc_right -= ARR_DELAY;
            }
        }
        /* Soft Drop */
        if (now - t_last_down < INPUT_KEEPALIVE) {
            acc_down += DELAY;
            while (acc_down >= SDF_DELAY) {
                move_piece(0, 1);
                acc_down -= SDF_DELAY;
            }
        }

        /* Check Grounded State & Lock Delay */
        int grounded = check_collision(current_piece.x, current_piece.y + 1, current_piece.rotation);
        
        if (grounded) {
            if (lock_timer == 0) {
                lock_timer = now;
            }
            if (now - lock_timer > LOCK_DELAY) {
                lock_piece();
                grounded = 0;
                lock_timer = 0;
            }
        } else {
            lock_timer = 0;
        }
        
        /* Gravity Logic */
        if (now - last_drop_time > drop_rate) {
            if (!grounded) {
                current_piece.y++;
            }
            last_drop_time = now;
        }

        draw_board();
        usleep(DELAY);
    }
}
