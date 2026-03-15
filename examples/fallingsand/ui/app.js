const canvas = document.getElementById('simCanvas');
const ctx = canvas.getContext('2d');

const cellSize = 4;
const empty = 0;
const sand = 1;
const water = 2;
const smoke = 3;

const palette = {
    [empty]: [7, 9, 12, 255],
    [sand]: [240, 192, 93, 255],
    [water]: [83, 166, 255, 255],
    [smoke]: [200, 208, 221, 180],
};

const toolButtons = document.querySelectorAll('.tool');
const brushSizeInput = document.getElementById('brushSize');
const spawnRateInput = document.getElementById('spawnRate');
const windInput = document.getElementById('wind');
const brushValue = document.getElementById('brushValue');
const spawnValue = document.getElementById('spawnValue');
const windValue = document.getElementById('windValue');
const pauseBtn = document.getElementById('pauseBtn');
const clearBtn = document.getElementById('clearBtn');
const randomBtn = document.getElementById('randomBtn');
const particleCount = document.getElementById('particleCount');
const activeMaterialLabel = document.getElementById('activeMaterial');
const simStatus = document.getElementById('simStatus');

let cols = 0;
let rows = 0;
let grid = new Uint8Array(0);
let nextGrid = new Uint8Array(0);
let imageData = null;
let drawing = false;
let paused = false;
let activeMaterial = 'sand';
let lastTimestamp = 0;
let accumTime = 0;

function materialNameToId(name) {
    switch (name) {
        case 'sand':
            return sand;
        case 'water':
            return water;
        case 'smoke':
            return smoke;
        default:
            return empty;
    }
}

function resizeSimulation() {
    const rect = canvas.getBoundingClientRect();
    const width = Math.max(cellSize, Math.floor(rect.width / cellSize) * cellSize);
    const height = Math.max(cellSize, Math.floor(rect.height / cellSize) * cellSize);

    canvas.width = width;
    canvas.height = height;
    cols = Math.floor(width / cellSize);
    rows = Math.floor(height / cellSize);
    grid = new Uint8Array(cols * rows);
    nextGrid = new Uint8Array(cols * rows);
    imageData = ctx.createImageData(width, height);
    seedFloor();
    drawGrid();
}

function seedFloor() {
    for (let x = 0; x < cols; x += 1) {
        if (Math.random() < 0.15) {
            const y = rows - 1 - Math.floor(Math.random() * 4);
            grid[index(x, y)] = sand;
        }
    }
}

function index(x, y) {
    return y * cols + x;
}

function inBounds(x, y) {
    return x >= 0 && x < cols && y >= 0 && y < rows;
}

function getCell(source, x, y) {
    if (!inBounds(x, y)) {
        return sand;
    }
    return source[index(x, y)];
}

function setCell(target, x, y, value) {
    if (!inBounds(x, y)) {
        return;
    }
    target[index(x, y)] = value;
}

function canDisplace(source, x, y, mover) {
    const occupant = getCell(source, x, y);
    if (occupant === empty) {
        return true;
    }
    if (mover === sand) {
        return occupant === water || occupant === smoke;
    }
    if (mover === water) {
        return occupant === smoke;
    }
    return false;
}

function moveParticle(source, target, x, y, nx, ny, value) {
    const destination = getCell(source, nx, ny);
    setCell(target, nx, ny, value);
    if (destination !== empty && destination !== value) {
        setCell(target, x, y, destination);
        return;
    }
    setCell(target, x, y, empty);
}

function updateSand(source, target, x, y) {
    if (canDisplace(source, x, y + 1, sand)) {
        moveParticle(source, target, x, y, x, y + 1, sand);
        return;
    }

    const directions = Math.random() < 0.5 ? [-1, 1] : [1, -1];
    for (const dx of directions) {
        if (canDisplace(source, x + dx, y + 1, sand)) {
            moveParticle(source, target, x, y, x + dx, y + 1, sand);
            return;
        }
    }

    setCell(target, x, y, sand);
}

function updateWater(source, target, x, y, wind) {
    if (canDisplace(source, x, y + 1, water)) {
        moveParticle(source, target, x, y, x, y + 1, water);
        return;
    }

    const lateral = wind === 0
        ? (Math.random() < 0.5 ? [-1, 1, -2, 2] : [1, -1, 2, -2])
        : [wind, wind * 2, -wind, -wind * 2];

    for (const dx of lateral) {
        if (canDisplace(source, x + dx, y, water)) {
            moveParticle(source, target, x, y, x + dx, y, water);
            return;
        }
        if (canDisplace(source, x + dx, y + 1, water)) {
            moveParticle(source, target, x, y, x + dx, y + 1, water);
            return;
        }
    }

    setCell(target, x, y, water);
}

function updateSmoke(source, target, x, y, wind) {
    if (y <= 0) {
        setCell(target, x, y, empty);
        return;
    }

    if (getCell(source, x, y - 1) === empty) {
        moveParticle(source, target, x, y, x, y - 1, smoke);
        return;
    }

    const drift = wind === 0
        ? (Math.random() < 0.5 ? [-1, 1] : [1, -1])
        : [wind, -wind];

    for (const dx of drift) {
        if (getCell(source, x + dx, y - 1) === empty) {
            moveParticle(source, target, x, y, x + dx, y - 1, smoke);
            return;
        }
        if (getCell(source, x + dx, y) === empty) {
            moveParticle(source, target, x, y, x + dx, y, smoke);
            return;
        }
    }

    if (Math.random() < 0.01) {
        setCell(target, x, y, empty);
        return;
    }

    setCell(target, x, y, smoke);
}

function stepSimulation() {
    nextGrid.fill(empty);
    const wind = Number(windInput.value);

    for (let y = rows - 1; y >= 0; y -= 1) {
        const leftToRight = y % 2 === 0;
        const start = leftToRight ? 0 : cols - 1;
        const end = leftToRight ? cols : -1;
        const delta = leftToRight ? 1 : -1;

        for (let x = start; x !== end; x += delta) {
            const value = getCell(grid, x, y);
            if (value === empty) {
                continue;
            }
            if (getCell(nextGrid, x, y) !== empty) {
                continue;
            }

            switch (value) {
                case sand:
                    updateSand(grid, nextGrid, x, y);
                    break;
                case water:
                    updateWater(grid, nextGrid, x, y, wind);
                    break;
                case smoke:
                    updateSmoke(grid, nextGrid, x, y, wind);
                    break;
                default:
                    setCell(nextGrid, x, y, value);
                    break;
            }
        }
    }

    const swap = grid;
    grid = nextGrid;
    nextGrid = swap;
}

function drawGrid() {
    const pixels = imageData.data;
    let count = 0;

    for (let y = 0; y < rows; y += 1) {
        for (let x = 0; x < cols; x += 1) {
            const value = getCell(grid, x, y);
            if (value !== empty) {
                count += 1;
            }
            const [r, g, b, a] = palette[value];
            const px = x * cellSize;
            const py = y * cellSize;

            for (let yy = 0; yy < cellSize; yy += 1) {
                for (let xx = 0; xx < cellSize; xx += 1) {
                    const offset = ((py + yy) * canvas.width + (px + xx)) * 4;
                    pixels[offset] = r;
                    pixels[offset + 1] = g;
                    pixels[offset + 2] = b;
                    pixels[offset + 3] = a;
                }
            }
        }
    }

    ctx.putImageData(imageData, 0, 0);
    particleCount.textContent = count.toLocaleString();
}

function paintAt(clientX, clientY) {
    const rect = canvas.getBoundingClientRect();
    const x = Math.floor((clientX - rect.left) / cellSize);
    const y = Math.floor((clientY - rect.top) / cellSize);
    const radius = Number(brushSizeInput.value);
    const material = materialNameToId(activeMaterial);
    const density = Number(spawnRateInput.value);

    for (let dy = -radius; dy <= radius; dy += 1) {
        for (let dx = -radius; dx <= radius; dx += 1) {
            if (dx * dx + dy * dy > radius * radius) {
                continue;
            }
            if (Math.random() * 12 > density) {
                continue;
            }
            setCell(grid, x + dx, y + dy, material);
        }
    }

    drawGrid();
}

function randomizeScene() {
    grid.fill(empty);
    for (let y = 0; y < rows; y += 1) {
        for (let x = 0; x < cols; x += 1) {
            const roll = Math.random();
            if (roll < 0.08) {
                setCell(grid, x, y, sand);
            } else if (roll < 0.12) {
                setCell(grid, x, y, water);
            } else if (roll < 0.135) {
                setCell(grid, x, y, smoke);
            }
        }
    }
    drawGrid();
}

function updateLabels() {
    brushValue.textContent = brushSizeInput.value;
    spawnValue.textContent = spawnRateInput.value;
    windValue.textContent = windInput.value;
    activeMaterialLabel.textContent = activeMaterial;
    simStatus.textContent = paused ? 'paused' : 'running';
    pauseBtn.textContent = paused ? 'Resume' : 'Pause';
}

function setActiveTool(name) {
    activeMaterial = name;
    for (const button of toolButtons) {
        button.classList.toggle('active', button.dataset.material === name);
    }
    updateLabels();
}

function animate(timestamp) {
    requestAnimationFrame(animate);
    if (!lastTimestamp) {
        lastTimestamp = timestamp;
    }

    const delta = timestamp - lastTimestamp;
    lastTimestamp = timestamp;
    accumTime += delta;

    if (paused || accumTime < 1000 / 60) {
        return;
    }

    accumTime = 0;
    stepSimulation();
    drawGrid();
}

toolButtons.forEach((button) => {
    button.addEventListener('click', () => {
        setActiveTool(button.dataset.material);
    });
});

brushSizeInput.addEventListener('input', updateLabels);
spawnRateInput.addEventListener('input', updateLabels);
windInput.addEventListener('input', updateLabels);

pauseBtn.addEventListener('click', () => {
    paused = !paused;
    updateLabels();
});

clearBtn.addEventListener('click', () => {
    grid.fill(empty);
    drawGrid();
});

randomBtn.addEventListener('click', randomizeScene);

canvas.addEventListener('pointerdown', (event) => {
    drawing = true;
    paintAt(event.clientX, event.clientY);
});

canvas.addEventListener('pointermove', (event) => {
    if (!drawing) {
        return;
    }
    paintAt(event.clientX, event.clientY);
});

window.addEventListener('pointerup', () => {
    drawing = false;
});

window.addEventListener('resize', resizeSimulation);

updateLabels();
resizeSimulation();
requestAnimationFrame(animate);