"use strict";

const canvas = document.getElementById("fire");
const ctx = canvas.getContext("2d");
const windSlider = document.getElementById("wind");
const windLabel = document.getElementById("wind-label");
const decaySlider = document.getElementById("decay");
const decayLabel = document.getElementById("decay-label");
const toggleBtn = document.getElementById("toggle");

// Fire dimensions (low resolution, scaled up via CSS image-rendering: pixelated).
const FIRE_W = 320;
const FIRE_H = 168;

// 37-color palette from black → red → orange → yellow → white,
// matching the classic PSX Doom fire lookup table.
const palette = [
    [7, 7, 7], [31, 7, 7], [47, 15, 7], [71, 15, 7],
    [87, 23, 7], [103, 31, 7], [119, 31, 7], [143, 39, 7],
    [159, 47, 7], [175, 63, 7], [191, 71, 7], [199, 71, 7],
    [223, 79, 7], [223, 87, 7], [223, 87, 7], [215, 95, 7],
    [215, 95, 7], [215, 103, 15], [207, 111, 15], [207, 119, 15],
    [207, 127, 15], [207, 135, 23], [199, 135, 23], [199, 143, 23],
    [199, 151, 31], [191, 159, 31], [191, 159, 31], [191, 167, 39],
    [191, 167, 39], [191, 175, 47], [183, 175, 47], [183, 183, 47],
    [183, 183, 55], [207, 207, 111], [223, 223, 159], [239, 239, 199],
    [255, 255, 255],
];

const NUM_COLORS = palette.length;

let firePixels = new Uint8Array(FIRE_W * FIRE_H);
let wind = 0;
let maxDecay = 2;
let lit = true;
let imgData = null;

function init() {
    canvas.width = FIRE_W;
    canvas.height = FIRE_H;
    imgData = ctx.createImageData(FIRE_W, FIRE_H);
    ignite();
}

// Set the bottom row to maximum intensity.
function ignite() {
    const base = (FIRE_H - 1) * FIRE_W;
    for (let x = 0; x < FIRE_W; x++) {
        firePixels[base + x] = NUM_COLORS - 1;
    }
    lit = true;
    toggleBtn.textContent = "Extinguish";
}

// Set the bottom row to zero (fire dies out naturally).
function extinguish() {
    const base = (FIRE_H - 1) * FIRE_W;
    for (let x = 0; x < FIRE_W; x++) {
        firePixels[base + x] = 0;
    }
    lit = false;
    toggleBtn.textContent = "Ignite";
}

function update() {
    // Propagate fire upward: for each pixel (except bottom row),
    // sample the pixel below, apply random decay and wind offset.
    for (let x = 0; x < FIRE_W; x++) {
        for (let y = 1; y < FIRE_H; y++) {
            const src = y * FIRE_W + x;
            const below = (y + 1 < FIRE_H) ? (y + 1) * FIRE_W + x : src;

            const decay = Math.floor(Math.random() * (maxDecay + 1));
            const windOff = Math.floor(Math.random() * 3) - 1 + wind;
            const dstX = Math.min(FIRE_W - 1, Math.max(0, x + windOff));
            const dst = (y - 1) * FIRE_W + dstX;

            const newVal = firePixels[src] - decay;
            firePixels[dst] = Math.max(0, newVal);
        }
    }
}

function draw() {
    const data = imgData.data;
    for (let i = 0; i < FIRE_W * FIRE_H; i++) {
        const c = palette[firePixels[i]];
        const off = i * 4;
        data[off] = c[0];
        data[off + 1] = c[1];
        data[off + 2] = c[2];
        data[off + 3] = 255;
    }
    ctx.putImageData(imgData, 0, 0);
}

function frame() {
    update();
    draw();
    requestAnimationFrame(frame);
}

// Controls.
windSlider.addEventListener("input", () => {
    wind = parseInt(windSlider.value, 10);
    windLabel.textContent = "Wind: " + wind;
});

decaySlider.addEventListener("input", () => {
    maxDecay = parseInt(decaySlider.value, 10);
    decayLabel.textContent = "Decay: " + maxDecay;
});

toggleBtn.addEventListener("click", () => {
    if (lit) {
        extinguish();
    } else {
        ignite();
    }
});

// Boot.
init();
requestAnimationFrame(frame);
