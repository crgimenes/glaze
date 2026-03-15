"use strict";

const canvas = document.getElementById("grid");
const ctx = canvas.getContext("2d");
const btnPlay = document.getElementById("btnPlay");
const btnStep = document.getElementById("btnStep");
const btnRandom = document.getElementById("btnRandom");
const btnClear = document.getElementById("btnClear");
const speedSlider = document.getElementById("speed");
const genCountEl = document.getElementById("genCount");

const GAP = 1;
const ALIVE_COLOR = "#39d353";
const DEAD_COLOR = "#161b22";
const GRID_COLOR = "#0a0a0f";

let cellSize = 12;
let grid = [];
let cols = 60;
let rows = 40;
let generation = 0;
let playing = false;
let tickTimer = null;
let drawing = false;

function resizeCanvas() {
    const toolbar = document.querySelector(".toolbar");
    const info = document.querySelector("p.info");
    const toolbarH = toolbar ? toolbar.offsetHeight : 0;
    const infoH = info ? info.offsetHeight : 0;
    const availW = window.innerWidth;
    const availH = window.innerHeight - toolbarH - infoH - 12;

    const maxCellW = (availW - GAP) / cols - GAP;
    const maxCellH = (availH - GAP) / rows - GAP;
    cellSize = Math.max(2, Math.floor(Math.min(maxCellW, maxCellH)));

    canvas.width = cols * (cellSize + GAP) + GAP;
    canvas.height = rows * (cellSize + GAP) + GAP;
}

async function init() {
    const size = await window.game_get_size();
    cols = size[0];
    rows = size[1];
    resizeCanvas();
    grid = await window.game_init();
    generation = 0;
    updateGen();
    draw();

    // Auto-start playing
    playing = true;
    await window.game_set_running(true);
    btnPlay.textContent = "\u23F8 Pause";
    btnPlay.classList.add("active");
    startTicking();
}

window.addEventListener("resize", () => {
    resizeCanvas();
    draw();
});

function draw() {
    ctx.fillStyle = GRID_COLOR;
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    for (let y = 0; y < rows; y++) {
        for (let x = 0; x < cols; x++) {
            ctx.fillStyle = grid[y][x] ? ALIVE_COLOR : DEAD_COLOR;
            ctx.fillRect(
                GAP + x * (cellSize + GAP),
                GAP + y * (cellSize + GAP),
                cellSize, cellSize
            );
        }
    }
}

function updateGen() {
    genCountEl.textContent = "Gen: " + generation;
}

function cellFromEvent(e) {
    const rect = canvas.getBoundingClientRect();
    const sx = canvas.width / rect.width;
    const sy = canvas.height / rect.height;
    const px = (e.clientX - rect.left) * sx;
    const py = (e.clientY - rect.top) * sy;
    const x = Math.floor(px / (cellSize + GAP));
    const y = Math.floor(py / (cellSize + GAP));
    if (x >= 0 && x < cols && y >= 0 && y < rows) {
        return { x, y };
    }
    return null;
}

// Track toggled cells during a drag to avoid flipping the same cell repeatedly.
let toggled = new Set();

canvas.addEventListener("pointerdown", async (e) => {
    drawing = true;
    toggled.clear();
    const c = cellFromEvent(e);
    if (c) {
        const key = c.x + "," + c.y;
        toggled.add(key);
        grid = await window.game_toggle(c.x, c.y);
        draw();
    }
    canvas.setPointerCapture(e.pointerId);
});

canvas.addEventListener("pointermove", async (e) => {
    if (!drawing) return;
    const c = cellFromEvent(e);
    if (!c) return;
    const key = c.x + "," + c.y;
    if (toggled.has(key)) return;
    toggled.add(key);
    grid = await window.game_toggle(c.x, c.y);
    draw();
});

canvas.addEventListener("pointerup", () => {
    drawing = false;
    toggled.clear();
});

function startTicking() {
    stopTicking();
    const fps = parseInt(speedSlider.value, 10);
    const ms = Math.max(16, Math.round(1000 / fps));
    tickTimer = setInterval(async () => {
        const result = await window.game_tick();
        if (result) {
            grid = result;
            generation++;
            updateGen();
            draw();
        }
    }, ms);
}

function stopTicking() {
    if (tickTimer !== null) {
        clearInterval(tickTimer);
        tickTimer = null;
    }
}

btnPlay.addEventListener("click", async () => {
    playing = !playing;
    await window.game_set_running(playing);
    btnPlay.textContent = playing ? "\u23F8 Pause" : "\u25B6 Play";
    btnPlay.classList.toggle("active", playing);
    if (playing) {
        startTicking();
    } else {
        stopTicking();
    }
});

btnStep.addEventListener("click", async () => {
    grid = await window.game_step();
    generation++;
    updateGen();
    draw();
});

btnRandom.addEventListener("click", async () => {
    grid = await window.game_init();
    generation = 0;
    updateGen();
    draw();
});

btnClear.addEventListener("click", async () => {
    playing = false;
    stopTicking();
    await window.game_set_running(false);
    btnPlay.textContent = "\u25B6 Play";
    btnPlay.classList.remove("active");
    grid = await window.game_clear();
    generation = 0;
    updateGen();
    draw();
});

speedSlider.addEventListener("input", () => {
    if (playing) startTicking();
});

init();
