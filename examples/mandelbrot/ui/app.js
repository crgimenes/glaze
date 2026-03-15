document.addEventListener('DOMContentLoaded', () => {
    const canvas = document.getElementById('fractalCanvas');
    const ctx = canvas.getContext('2d');
    const autoZoomTarget = { x: -0.743643887037151, y: 0.13182590420533 };
    const autoZoomRate = 0.38;
    const autoPanRate = 0.45;
    const maxAutoZoom = 1e8;
    const targetFrameTime = 1000 / 24;

    const fractalTypeRadios = document.querySelectorAll('input[name="fractalType"]');
    const juliaControls = document.getElementById('julia-controls');
    const cxSlider = document.getElementById('cx');
    const cySlider = document.getElementById('cy');
    const iterationsSlider = document.getElementById('iterations');
    const colorSchemeSelect = document.getElementById('color-scheme');
    const resetBtn = document.getElementById('reset-btn');

    const zoomLevelSpan = document.getElementById('zoom-level');
    const centerXSpan = document.getElementById('center-x');
    const centerYSpan = document.getElementById('center-y');
    const mouseXSpan = document.getElementById('mouse-x');
    const mouseYSpan = document.getElementById('mouse-y');
    const cxValueSpan = document.getElementById('cx-value');
    const cyValueSpan = document.getElementById('cy-value');
    const iterationsValueSpan = document.getElementById('iterations-value');

    function createDefaultState() {
        return {
            fractalType: 'mandelbrot',
            maxIterations: 100,
            zoom: 1.0,
            centerX: -0.5,
            centerY: 0,
            juliaC: { x: -0.7, y: 0.27015 },
            colorScheme: 'blue-gold',
            isDragging: false,
            lastMousePos: { x: 0, y: 0 },
        };
    }

    let state = createDefaultState();
    let lastFrameTime = 0;
    let renderInFlight = false;
    let renderQueued = false;
    let renderVersion = 0;
    let renderController = null;
    let lastCanvasWidth = 0;
    let lastCanvasHeight = 0;
    let resizeTimer = null;
    let isResizing = false;
    let resizeObserver = null;

    function buildRenderURL() {
        const params = new URLSearchParams({
            fractal: state.fractalType,
            color: state.colorScheme,
            width: String(canvas.width),
            height: String(canvas.height),
            iterations: String(state.maxIterations),
            zoom: String(state.zoom),
            centerX: String(state.centerX),
            centerY: String(state.centerY),
            juliaCX: String(state.juliaC.x),
            juliaCY: String(state.juliaC.y),
        });

        return `${window.location.origin}/render?${params.toString()}`;
    }

    function restartView() {
        state = createDefaultState();
        syncControls();
    }

    function cancelRender() {
        renderVersion += 1;
        renderQueued = false;
        if (renderController) {
            renderController.abort();
            renderController = null;
        }
        renderInFlight = false;
    }

    async function drawFractal() {
        const width = canvas.width;
        const height = canvas.height;
        if (!width || !height) {
            return;
        }

        const requestVersion = ++renderVersion;
        const controller = new AbortController();
        renderController = controller;
        renderInFlight = true;

        try {
            const response = await fetch(buildRenderURL(), { signal: controller.signal });
            if (!response.ok) {
                throw new Error(`render failed with status ${response.status}`);
            }

            const buffer = await response.arrayBuffer();
            if (requestVersion !== renderVersion) {
                return;
            }

            if (width !== canvas.width || height !== canvas.height) {
                requestRender();
                return;
            }

            const pixels = new Uint8ClampedArray(buffer);
            const imageData = new ImageData(pixels, width, height);
            ctx.putImageData(imageData, 0, 0);
        } catch (error) {
            if (error.name !== 'AbortError') {
                console.error(error);
            }
        } finally {
            if (renderController === controller) {
                renderController = null;
            }
            renderInFlight = false;
            if (renderQueued) {
                renderQueued = false;
                requestRender();
            }
        }

        updateInfo();
    }

    function finishResize() {
        const nextWidth = canvas.clientWidth;
        const nextHeight = canvas.clientHeight;
        if (!nextWidth || !nextHeight) {
            isResizing = false;
            return;
        }

        const previousArea = lastCanvasWidth * lastCanvasHeight;
        const nextArea = nextWidth * nextHeight;
        const grewSignificantly = previousArea > 0 && nextArea > previousArea * 1.35;

        canvas.width = nextWidth;
        canvas.height = nextHeight;
        lastCanvasWidth = canvas.width;
        lastCanvasHeight = canvas.height;
        isResizing = false;

        if (grewSignificantly) {
            restartView();
        }

        requestRender();
    }

    function scheduleResize() {
        isResizing = true;
        cancelRender();
        if (resizeTimer) {
            window.clearTimeout(resizeTimer);
        }
        resizeTimer = window.setTimeout(() => {
            resizeTimer = null;
            finishResize();
        }, 120);
    }

    function requestRender() {
        if (renderInFlight) {
            renderQueued = true;
            return;
        }

        void drawFractal();
    }

    function updateInfo() {
        zoomLevelSpan.textContent = state.zoom.toFixed(2);
        centerXSpan.textContent = state.centerX.toExponential(2);
        centerYSpan.textContent = state.centerY.toExponential(2);
        cxValueSpan.textContent = state.juliaC.x.toFixed(4);
        cyValueSpan.textContent = state.juliaC.y.toFixed(4);
        iterationsValueSpan.textContent = state.maxIterations;
    }

    function syncControls() {
        cxSlider.value = state.juliaC.x;
        cySlider.value = state.juliaC.y;
        iterationsSlider.value = state.maxIterations;
        colorSchemeSelect.value = state.colorScheme;
        document.querySelector(`input[name="fractalType"][value="${state.fractalType}"]`).checked = true;
        juliaControls.style.display = state.fractalType === 'julia' ? 'block' : 'none';
        updateInfo();
    }

    function updateAnimatedIterations() {
        const minimumIterations = Math.min(
            1000,
            100 + Math.floor(Math.log2(Math.max(state.zoom, 1)) * 18),
        );
        if (state.maxIterations >= minimumIterations) {
            return;
        }

        state.maxIterations = minimumIterations;
        iterationsSlider.value = state.maxIterations;
    }

    function stepAnimation(deltaSeconds) {
        if (state.zoom >= maxAutoZoom) {
            state = createDefaultState();
            syncControls();
            return;
        }

        const panBlend = Math.min(0.08, deltaSeconds * autoPanRate);
        state.centerX += (autoZoomTarget.x - state.centerX) * panBlend;
        state.centerY += (autoZoomTarget.y - state.centerY) * panBlend;
        state.zoom *= Math.exp(autoZoomRate * deltaSeconds);
        updateAnimatedIterations();
    }

    function animate(now) {
        requestAnimationFrame(animate);
        if (!lastFrameTime) {
            lastFrameTime = now;
        }

        const elapsed = now - lastFrameTime;
        if (elapsed < targetFrameTime) {
            return;
        }

        lastFrameTime = now;
        if (isResizing) {
            return;
        }

        if (!state.isDragging) {
            stepAnimation(elapsed / 1000);
        }
        requestRender();
    }

    function resizeCanvas() {
        scheduleResize();
    }

    window.addEventListener('resize', resizeCanvas);

    canvas.addEventListener('wheel', (e) => {
        e.preventDefault();
        const rect = canvas.getBoundingClientRect();
        const mouseX = e.clientX - rect.left;
        const mouseY = e.clientY - rect.top;
        const scale = 4.0 / (canvas.width * state.zoom);
        const mouseCoordX = (mouseX - canvas.width / 2) * scale + state.centerX;
        const mouseCoordY = (mouseY - canvas.height / 2) * scale + state.centerY;
        const zoomFactor = e.deltaY < 0 ? 1.2 : 1 / 1.2;

        state.zoom *= zoomFactor;
        state.centerX = mouseCoordX - (mouseX - canvas.width / 2) * (scale / zoomFactor);
        state.centerY = mouseCoordY - (mouseY - canvas.height / 2) * (scale / zoomFactor);
        requestRender();
    });

    canvas.addEventListener('mousedown', (e) => {
        state.isDragging = true;
        state.lastMousePos = { x: e.clientX, y: e.clientY };
    });

    canvas.addEventListener('mouseup', () => {
        state.isDragging = false;
    });

    canvas.addEventListener('mouseleave', () => {
        state.isDragging = false;
    });

    canvas.addEventListener('mousemove', (e) => {
        const rect = canvas.getBoundingClientRect();
        const mouseX = e.clientX - rect.left;
        const mouseY = e.clientY - rect.top;
        const scale = 4.0 / (canvas.width * state.zoom);
        const coordX = (mouseX - canvas.width / 2) * scale + state.centerX;
        const coordY = (mouseY - canvas.height / 2) * scale + state.centerY;

        mouseXSpan.textContent = coordX.toFixed(4);
        mouseYSpan.textContent = coordY.toFixed(4);

        if (!state.isDragging) {
            return;
        }

        const dx = e.clientX - state.lastMousePos.x;
        const dy = e.clientY - state.lastMousePos.y;
        state.centerX -= dx * scale;
        state.centerY -= dy * scale;
        state.lastMousePos = { x: e.clientX, y: e.clientY };
        requestRender();
    });

    fractalTypeRadios.forEach((radio) => {
        radio.addEventListener('change', (e) => {
            state.fractalType = e.target.value;
            juliaControls.style.display = state.fractalType === 'julia' ? 'block' : 'none';
            requestRender();
        });
    });

    cxSlider.addEventListener('input', (e) => {
        state.juliaC.x = parseFloat(e.target.value);
        if (state.fractalType !== 'julia') {
            return;
        }

        requestRender();
    });

    cySlider.addEventListener('input', (e) => {
        state.juliaC.y = parseFloat(e.target.value);
        if (state.fractalType !== 'julia') {
            return;
        }

        requestRender();
    });

    iterationsSlider.addEventListener('input', (e) => {
        state.maxIterations = parseInt(e.target.value, 10);
        requestRender();
    });

    colorSchemeSelect.addEventListener('change', (e) => {
        state.colorScheme = e.target.value;
        requestRender();
    });

    resetBtn.addEventListener('click', () => {
        restartView();
        requestRender();
    });

    if (typeof ResizeObserver === 'function') {
        resizeObserver = new ResizeObserver(() => {
            scheduleResize();
        });
        resizeObserver.observe(canvas);
        resizeObserver.observe(document.body);
    }

    syncControls();
    resizeCanvas();
    requestAnimationFrame(animate);
});
