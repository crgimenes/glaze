"use strict";

const canvas = document.getElementById("space");
const ctx = canvas.getContext("2d");
const speedSlider = document.getElementById("speed");
const speedLabel = document.getElementById("speed-label");
const starsSlider = document.getElementById("stars");
const starsLabel = document.getElementById("stars-label");

const MAX_DEPTH = 1000;

let W = 0;
let H = 0;
let stars = [];
let numStars = 800;
let speed = 5;

// Mouse influence: 0,0 = center (fly forward), range roughly -1..1.
let mouseInfluence = { x: 0, y: 0 };
let mouseOver = false;

function resize() {
    W = window.innerWidth;
    H = window.innerHeight;
    canvas.width = W;
    canvas.height = H;
}

function makeStar() {
    return {
        x: (Math.random() - 0.5) * W * 2,
        y: (Math.random() - 0.5) * H * 2,
        z: Math.random() * MAX_DEPTH,
    };
}

function initStars() {
    stars = [];
    for (let i = 0; i < numStars; i++) {
        stars.push(makeStar());
    }
}

function adjustStarCount() {
    while (stars.length < numStars) {
        const s = makeStar();
        s.z = MAX_DEPTH;
        stars.push(s);
    }
    if (stars.length > numStars) {
        stars.length = numStars;
    }
}

function update() {
    const cx = W / 2;
    const cy = H / 2;

    // Drift offset: mouse influence shifts the vanishing point.
    const driftX = mouseOver ? mouseInfluence.x * speed * 2 : 0;
    const driftY = mouseOver ? mouseInfluence.y * speed * 2 : 0;

    for (let i = 0; i < stars.length; i++) {
        const s = stars[i];
        s.z -= speed;

        // Lateral drift based on mouse position.
        s.x -= driftX;
        s.y -= driftY;

        // Recycle stars that pass behind the camera or go too far off-screen.
        if (s.z <= 0) {
            s.x = (Math.random() - 0.5) * W * 2;
            s.y = (Math.random() - 0.5) * H * 2;
            s.z = MAX_DEPTH;
        }
    }
}

function draw() {
    // Fade trail effect.
    ctx.fillStyle = "rgba(0, 0, 0, 0.25)";
    ctx.fillRect(0, 0, W, H);

    const cx = W / 2;
    const cy = H / 2;

    for (let i = 0; i < stars.length; i++) {
        const s = stars[i];

        // Perspective projection.
        const sx = cx + s.x / s.z * cx;
        const sy = cy + s.y / s.z * cy;

        // Skip off-screen stars.
        if (sx < -10 || sx > W + 10 || sy < -10 || sy > H + 10) {
            continue;
        }

        // Size and brightness scale with proximity.
        const t = 1 - s.z / MAX_DEPTH;
        const radius = Math.max(0.3, t * 3);
        const brightness = Math.floor(80 + t * 175);

        // Slight blue-white color shift for closer stars.
        const r = brightness;
        const g = brightness;
        const b = Math.min(255, brightness + Math.floor(t * 40));

        ctx.beginPath();
        ctx.arc(sx, sy, radius, 0, Math.PI * 2);
        ctx.fillStyle = "rgb(" + r + "," + g + "," + b + ")";
        ctx.fill();

        // Streak line for fast/close stars.
        if (t > 0.3 && speed > 2) {
            const streakLen = t * speed * 1.5;
            const prevZ = s.z + speed;
            const px = cx + s.x / prevZ * cx;
            const py = cy + s.y / prevZ * cy;
            ctx.beginPath();
            ctx.moveTo(px, py);
            ctx.lineTo(sx, sy);
            ctx.strokeStyle = "rgba(" + r + "," + g + "," + b + "," + (t * 0.6) + ")";
            ctx.lineWidth = Math.max(0.5, radius * 0.6);
            ctx.stroke();
        }
    }
}

function frame() {
    update();
    draw();
    requestAnimationFrame(frame);
}

// Mouse tracking.
canvas.addEventListener("mousemove", (e) => {
    mouseOver = true;
    mouseInfluence.x = (e.clientX / W - 0.5) * 2;
    mouseInfluence.y = (e.clientY / H - 0.5) * 2;
});

canvas.addEventListener("mouseleave", () => {
    mouseOver = false;
    mouseInfluence.x = 0;
    mouseInfluence.y = 0;
});

// Controls.
speedSlider.addEventListener("input", () => {
    speed = parseInt(speedSlider.value, 10);
    speedLabel.textContent = "Speed: " + speed;
});

starsSlider.addEventListener("input", () => {
    numStars = parseInt(starsSlider.value, 10);
    starsLabel.textContent = "Stars: " + numStars;
    adjustStarCount();
});

window.addEventListener("resize", resize);

// Boot.
resize();
initStars();

// Clear canvas to black before first frame.
ctx.fillStyle = "#000";
ctx.fillRect(0, 0, W, H);

requestAnimationFrame(frame);
