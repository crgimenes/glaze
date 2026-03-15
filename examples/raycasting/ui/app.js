const canvas = document.getElementById('viewCanvas');
const ctx = canvas.getContext('2d');
const sceneCanvas = document.createElement('canvas');
const sceneCtx = sceneCanvas.getContext('2d');

const positionValue = document.getElementById('positionValue');
const headingValue = document.getElementById('headingValue');
const fpsValue = document.getElementById('fpsValue');
const distanceRange = document.getElementById('distanceRange');
const distanceValue = document.getElementById('distanceValue');
const fovRange = document.getElementById('fovRange');
const fovValue = document.getElementById('fovValue');
const toggleMapBtn = document.getElementById('toggleMapBtn');
const shuffleBtn = document.getElementById('shuffleBtn');
const resetBtn = document.getElementById('resetBtn');
const movementKeys = new Set(['w', 'a', 's', 'd', 'q', 'e', 'arrowleft', 'arrowright']);

const mapGrid = [
    [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1],
    [1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1],
    [1, 0, 2, 0, 0, 3, 0, 0, 4, 0, 0, 5, 0, 1],
    [1, 0, 0, 0, 1, 1, 1, 0, 0, 0, 1, 0, 0, 1],
    [1, 0, 0, 0, 0, 0, 1, 0, 6, 0, 1, 0, 0, 1],
    [1, 0, 1, 1, 0, 0, 1, 0, 0, 0, 1, 0, 0, 1],
    [1, 0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1],
    [1, 0, 1, 0, 7, 0, 0, 0, 1, 0, 0, 8, 0, 1],
    [1, 0, 0, 0, 1, 1, 1, 0, 1, 1, 1, 0, 0, 1],
    [1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 1],
    [1, 0, 9, 0, 0, 10, 0, 0, 11, 0, 0, 12, 0, 1],
    [1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1],
    [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1],
];

let wallPalette = buildPalette();
let showMiniMap = true;
let lastFrame = 0;
let fps = 0;

const player = {
    x: 1.5,
    y: 1.5,
    angle: 0,
    moveSpeed: 2.7,
    turnSpeed: 2.2,
};

const keyState = new Set();

function buildPalette() {
    return {
        1: '#5d7af0',
        2: '#f59e0b',
        3: '#10b981',
        4: '#ef4444',
        5: '#a855f7',
        6: '#ec4899',
        7: '#14b8a6',
        8: '#eab308',
        9: '#8b5cf6',
        10: '#22c55e',
        11: '#fb7185',
        12: '#06b6d4',
    };
}

function resizeCanvas() {
    const rect = canvas.getBoundingClientRect();
    canvas.width = Math.max(320, Math.floor(rect.width));
    canvas.height = Math.max(240, Math.floor(rect.height));

    const targetSceneWidth = Math.max(240, Math.floor(canvas.width / 3));
    const aspectRatio = canvas.height / canvas.width;
    sceneCanvas.width = targetSceneWidth;
    sceneCanvas.height = Math.max(160, Math.floor(targetSceneWidth * aspectRatio));
}

function normalizeAngle(angle) {
    const tau = Math.PI * 2;
    return (angle % tau + tau) % tau;
}

function wallAt(x, y) {
    const mx = Math.floor(x);
    const my = Math.floor(y);
    if (my < 0 || my >= mapGrid.length || mx < 0 || mx >= mapGrid[0].length) {
        return 1;
    }
    return mapGrid[my][mx];
}

function movePlayer(dx, dy) {
    const nextX = player.x + dx;
    const nextY = player.y + dy;
    if (wallAt(nextX, player.y) === 0) {
        player.x = nextX;
    }
    if (wallAt(player.x, nextY) === 0) {
        player.y = nextY;
    }
}

function updatePlayer(deltaSeconds) {
    let moveX = 0;
    let moveY = 0;
    if (keyState.has('arrowleft')) {
        player.angle -= player.turnSpeed * deltaSeconds;
    }
    if (keyState.has('arrowright')) {
        player.angle += player.turnSpeed * deltaSeconds;
    }
    if (keyState.has('a')) {
        player.angle -= player.turnSpeed * deltaSeconds;
    }
    if (keyState.has('d')) {
        player.angle += player.turnSpeed * deltaSeconds;
    }
    player.angle = normalizeAngle(player.angle);

    const forwardX = Math.cos(player.angle);
    const forwardY = Math.sin(player.angle);
    const rightX = Math.cos(player.angle + Math.PI / 2);
    const rightY = Math.sin(player.angle + Math.PI / 2);

    if (keyState.has('w')) {
        moveX += forwardX;
        moveY += forwardY;
    }
    if (keyState.has('s')) {
        moveX -= forwardX;
        moveY -= forwardY;
    }
    if (keyState.has('q')) {
        moveX -= rightX;
        moveY -= rightY;
    }
    if (keyState.has('e')) {
        moveX += rightX;
        moveY += rightY;
    }

    if (moveX !== 0 || moveY !== 0) {
        const length = Math.hypot(moveX, moveY) || 1;
        movePlayer(
            (moveX / length) * player.moveSpeed * deltaSeconds,
            (moveY / length) * player.moveSpeed * deltaSeconds,
        );
    }
}

function castRay(angle, maxDistance) {
    const rayDirX = Math.cos(angle);
    const rayDirY = Math.sin(angle);

    let mapX = Math.floor(player.x);
    let mapY = Math.floor(player.y);

    const deltaDistX = rayDirX === 0 ? Number.MAX_VALUE : Math.abs(1 / rayDirX);
    const deltaDistY = rayDirY === 0 ? Number.MAX_VALUE : Math.abs(1 / rayDirY);

    let stepX = 0;
    let stepY = 0;
    let sideDistX = 0;
    let sideDistY = 0;

    if (rayDirX < 0) {
        stepX = -1;
        sideDistX = (player.x - mapX) * deltaDistX;
    } else {
        stepX = 1;
        sideDistX = (mapX + 1 - player.x) * deltaDistX;
    }

    if (rayDirY < 0) {
        stepY = -1;
        sideDistY = (player.y - mapY) * deltaDistY;
    } else {
        stepY = 1;
        sideDistY = (mapY + 1 - player.y) * deltaDistY;
    }

    let hit = 0;
    let side = 0;
    let distance = 0;

    while (!hit && distance < maxDistance) {
        if (sideDistX < sideDistY) {
            sideDistX += deltaDistX;
            mapX += stepX;
            side = 0;
        } else {
            sideDistY += deltaDistY;
            mapY += stepY;
            side = 1;
        }

        hit = wallAt(mapX, mapY);
        if (side === 0) {
            distance = sideDistX - deltaDistX;
        } else {
            distance = sideDistY - deltaDistY;
        }
    }

    return {
        hit,
        side,
        distance,
        mapX,
        mapY,
        rayDirX,
        rayDirY,
    };
}

function hslToRgb(h, s, l) {
    const saturation = s / 100;
    const lightness = l / 100;
    const chroma = (1 - Math.abs(2 * lightness - 1)) * saturation;
    const segment = h / 60;
    const x = chroma * (1 - Math.abs((segment % 2) - 1));

    let r1 = 0;
    let g1 = 0;
    let b1 = 0;
    if (segment >= 0 && segment < 1) {
        r1 = chroma;
        g1 = x;
    } else if (segment < 2) {
        r1 = x;
        g1 = chroma;
    } else if (segment < 3) {
        g1 = chroma;
        b1 = x;
    } else if (segment < 4) {
        g1 = x;
        b1 = chroma;
    } else if (segment < 5) {
        r1 = x;
        b1 = chroma;
    } else {
        r1 = chroma;
        b1 = x;
    }

    const match = lightness - chroma / 2;
    return {
        r: Math.round((r1 + match) * 255),
        g: Math.round((g1 + match) * 255),
        b: Math.round((b1 + match) * 255),
    };
}

function colorToRgb(color) {
    if (color.startsWith('#')) {
        const normalized = color.length === 4
            ? `#${color[1]}${color[1]}${color[2]}${color[2]}${color[3]}${color[3]}`
            : color;
        const numeric = parseInt(normalized.slice(1), 16);
        return {
            r: (numeric >> 16) & 255,
            g: (numeric >> 8) & 255,
            b: numeric & 255,
        };
    }

    const hslMatch = color.match(/^hsl\((\d+),\s*(\d+)%?,\s*(\d+)%?\)$/i);
    if (hslMatch) {
        return hslToRgb(Number(hslMatch[1]), Number(hslMatch[2]), Number(hslMatch[3]));
    }

    return { r: 119, g: 119, b: 119 };
}

function shadeColor(color, shade) {
    const rgb = colorToRgb(color);
    const r = Math.min(255, Math.max(0, Math.round(rgb.r * shade)));
    const g = Math.min(255, Math.max(0, Math.round(rgb.g * shade)));
    const b = Math.min(255, Math.max(0, Math.round(rgb.b * shade)));
    return `rgb(${r}, ${g}, ${b})`;
}

function renderScene() {
    const width = sceneCanvas.width;
    const height = sceneCanvas.height;
    const maxDistance = Number(distanceRange.value);
    const fov = Number(fovRange.value) * (Math.PI / 180);
    const horizon = height / 2;

    sceneCtx.fillStyle = getComputedStyle(document.documentElement).getPropertyValue('--ceiling').trim();
    sceneCtx.fillRect(0, 0, width, horizon);
    sceneCtx.fillStyle = getComputedStyle(document.documentElement).getPropertyValue('--floor').trim();
    sceneCtx.fillRect(0, horizon, width, height - horizon);

    for (let column = 0; column < width; column += 1) {
        const cameraX = (column / width) * 2 - 1;
        const rayAngle = player.angle + cameraX * (fov / 2);
        const ray = castRay(rayAngle, maxDistance);
        const correctedDistance = Math.max(0.0001, ray.distance * Math.cos(rayAngle - player.angle));
        const wallHeight = Math.min(height, Math.floor(height / correctedDistance));
        const start = Math.floor((height - wallHeight) / 2);
        const end = start + wallHeight;
        const baseColor = wallPalette[ray.hit] || '#777777';
        const sideShade = ray.side === 1 ? 0.68 : 1;
        const distanceShade = Math.max(0.18, 1 - correctedDistance / (maxDistance * 1.35));
        sceneCtx.fillStyle = shadeColor(baseColor, sideShade * distanceShade);
        sceneCtx.fillRect(column, start, 1, end - start);
    }
}

function renderMiniMap() {
    const tile = 10;
    const padding = 16;
    const mapWidth = mapGrid[0].length * tile;
    const mapHeight = mapGrid.length * tile;

    ctx.save();
    ctx.globalAlpha = 0.86;
    ctx.fillStyle = 'rgba(8, 10, 14, 0.78)';
    ctx.fillRect(padding, padding, mapWidth + 16, mapHeight + 16);
    ctx.globalAlpha = 1;

    for (let y = 0; y < mapGrid.length; y += 1) {
        for (let x = 0; x < mapGrid[y].length; x += 1) {
            const cell = mapGrid[y][x];
            ctx.fillStyle = cell === 0 ? 'rgba(255,255,255,0.06)' : (wallPalette[cell] || '#888');
            ctx.fillRect(padding + 8 + x * tile, padding + 8 + y * tile, tile - 1, tile - 1);
        }
    }

    const px = padding + 8 + player.x * tile;
    const py = padding + 8 + player.y * tile;
    ctx.fillStyle = '#ffffff';
    ctx.beginPath();
    ctx.arc(px, py, 3.5, 0, Math.PI * 2);
    ctx.fill();

    ctx.strokeStyle = '#ffffff';
    ctx.lineWidth = 1.5;
    ctx.beginPath();
    ctx.moveTo(px, py);
    ctx.lineTo(px + Math.cos(player.angle) * 18, py + Math.sin(player.angle) * 18);
    ctx.stroke();
    ctx.restore();
}

function updateHUD() {
    positionValue.textContent = `${player.x.toFixed(2)}, ${player.y.toFixed(2)}`;
    headingValue.textContent = `${Math.round((normalizeAngle(player.angle) * 180) / Math.PI)}°`;
    fpsValue.textContent = String(Math.round(fps));
    distanceValue.textContent = distanceRange.value;
    fovValue.textContent = `${fovRange.value}°`;
    toggleMapBtn.textContent = showMiniMap ? 'Hide Mini Map' : 'Show Mini Map';
}

function drawFrame() {
    renderScene();
    ctx.imageSmoothingEnabled = false;
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    ctx.drawImage(sceneCanvas, 0, 0, canvas.width, canvas.height);
    if (showMiniMap) {
        renderMiniMap();
    }
    updateHUD();
}

function resetPlayer() {
    player.x = 1.5;
    player.y = 1.5;
    player.angle = 0;
}

function toggleMiniMap(event) {
    if (event) {
        event.preventDefault();
        event.stopPropagation();
    }
    showMiniMap = !showMiniMap;
    drawFrame();
}

function animate(timestamp) {
    requestAnimationFrame(animate);
    if (!lastFrame) {
        lastFrame = timestamp;
    }

    const deltaSeconds = Math.min((timestamp - lastFrame) / 1000, 0.05);
    lastFrame = timestamp;
    fps = fps * 0.9 + (1 / Math.max(deltaSeconds, 0.001)) * 0.1;

    updatePlayer(deltaSeconds);
    drawFrame();
}

window.addEventListener('keydown', (event) => {
    const key = event.key.toLowerCase();
    if (movementKeys.has(key)) {
        event.preventDefault();
    }
    keyState.add(key);
});

window.addEventListener('keyup', (event) => {
    const key = event.key.toLowerCase();
    if (movementKeys.has(key)) {
        event.preventDefault();
    }
    keyState.delete(key);
});

distanceRange.addEventListener('input', updateHUD);
fovRange.addEventListener('input', updateHUD);

toggleMapBtn.addEventListener('pointerdown', toggleMiniMap);
toggleMapBtn.addEventListener('click', (event) => {
    event.preventDefault();
    event.stopPropagation();
});

shuffleBtn.addEventListener('click', () => {
    wallPalette = buildPalette();
    const keys = Object.keys(wallPalette);
    for (const key of keys) {
        const hue = Math.floor(Math.random() * 360);
        const sat = 55 + Math.floor(Math.random() * 30);
        const light = 45 + Math.floor(Math.random() * 18);
        wallPalette[key] = `hsl(${hue}, ${sat}%, ${light}%)`;
    }
    drawFrame();
});

resetBtn.addEventListener('click', () => {
    resetPlayer();
    drawFrame();
});

canvas.tabIndex = 0;
canvas.addEventListener('pointerdown', () => {
    canvas.focus({ preventScroll: true });
});

window.addEventListener('resize', resizeCanvas);

resizeCanvas();
drawFrame();
requestAnimationFrame(animate);