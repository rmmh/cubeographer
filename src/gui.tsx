import { render as preactRender } from 'preact';
import { useState, useEffect, useRef } from 'preact/hooks';
import * as renderer from './renderer';
import { OrbitControls } from './camera';

interface DebugGUIProps {
    sceneGraph: renderer.SceneGraph;
    controls: OrbitControls;
    context: renderer.Context;
    render: () => void;
}

export function setupGUI(
    sceneGraph: renderer.SceneGraph,
    controls: OrbitControls,
    context: renderer.Context,
    render: () => void
) {
    const root = document.getElementById('gui-root');
    if (root) {
        // Clear anything that might be there
        root.innerHTML = '';
        preactRender(
            <DebugGUI
                sceneGraph={sceneGraph}
                controls={controls}
                context={context}
                render={render}
            />,
            root
        );
    }
}

function DebugGUI({ sceneGraph, controls, context, render }: DebugGUIProps) {
    const [collapsed, setCollapsed] = useState<boolean>(false);
    const [maxHighResChunks, setMaxHighResChunks] = useState<number>(sceneGraph.maxHighResChunks);
    const [inspectorMode, setInspectorMode] = useState<boolean>(false);
    const [showBoundaries, setShowBoundaries] = useState<boolean>(sceneGraph.showBoundaries);
    const [activeRegion, setActiveRegion] = useState<{ rx: number; rz: number } | null>(null);
    const [updateTick, setUpdateTick] = useState<number>(0);

    // Subscribe to SceneGraph updates to trigger GUI re-renders on streaming/loading changes
    useEffect(() => {
        return sceneGraph.subscribe(() => {
            setUpdateTick(tick => tick + 1);
        });
    }, [sceneGraph]);

    // Coordinate picking on canvas via pointer events
    useEffect(() => {
        const canvas = context.canvas;
        let mouseDownPos = { x: 0, y: 0 };
        let mouseDownTime = 0;

        const handlePointerDown = (e: PointerEvent) => {
            mouseDownPos = { x: e.clientX, y: e.clientY };
            mouseDownTime = Date.now();
        };

        const handlePointerUp = (e: PointerEvent) => {
            if (!inspectorMode) return;

            const dx = e.clientX - mouseDownPos.x;
            const dy = e.clientY - mouseDownPos.y;
            const dist = Math.sqrt(dx * dx + dy * dy);
            const timeElapsed = Date.now() - mouseDownTime;

            // Check if it's a clicked tap (less than 4px drag, less than 400ms duration)
            if (dist < 4 && timeElapsed < 400) {
                const orbitTargetFinder = controls.getOrbitTarget;
                if (!orbitTargetFinder) return;

                const worldPos = orbitTargetFinder(e.clientX, e.clientY);
                if (worldPos) {
                    const rx = Math.floor(worldPos[0] / 512);
                    const rz = Math.floor(worldPos[2] / 512);
                    setActiveRegion({ rx, rz });
                }
            }
        };

        canvas.addEventListener('pointerdown', handlePointerDown);
        canvas.addEventListener('pointerup', handlePointerUp);

        // Update canvas cursor based on active mode
        canvas.style.cursor = inspectorMode ? 'crosshair' : 'default';

        return () => {
            canvas.removeEventListener('pointerdown', handlePointerDown);
            canvas.removeEventListener('pointerup', handlePointerUp);
            canvas.style.cursor = 'default';
        };
    }, [inspectorMode, context, controls]);

    const handleToggleCollapse = (e: MouseEvent) => {
        e.stopPropagation();
        setCollapsed(!collapsed);
    };

    const handleMenuClick = () => {
        if (collapsed) {
            setCollapsed(false);
        }
    };

    const handleSliderChange = (e: any) => {
        const val = parseInt(e.target.value, 10);
        setMaxHighResChunks(val);
        sceneGraph.maxHighResChunks = val;
        render();
    };

    const handleInspectorToggleChange = (e: any) => {
        const checked = e.target.checked;
        setInspectorMode(checked);
        if (!checked) {
            setActiveRegion(null);
        }
    };

    const handleBoundariesToggleChange = (e: any) => {
        const checked = e.target.checked;
        setShowBoundaries(checked);
        sceneGraph.showBoundaries = checked;
        render();
    };

    const handleCloseInspector = () => {
        setActiveRegion(null);
        setInspectorMode(false);
    };

    return (
        <>
            {/* Debug Menu Console */}
            <div
                id="debug-menu"
                className={collapsed ? 'collapsed' : ''}
                onClick={handleMenuClick}
            >
                <div className="debug-menu-header">
                    <span className="debug-menu-title">DEBUG</span>
                    <button
                        className="debug-toggle-btn"
                        title="Collapse/Expand Menu"
                        onClick={handleToggleCollapse}
                    >
                        <svg
                            viewBox="0 0 24 24"
                            width="18"
                            height="18"
                            fill="none"
                            stroke="currentColor"
                            strokeWidth={2}
                        >
                            <path d="M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z" />
                            <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1Z" />
                        </svg>
                    </button>
                </div>
                <div className="debug-menu-content">
                    <div className="debug-control-group">
                        <div className="debug-label-row">
                            <span>Max High-Res Regionlets</span>
                            <span className="debug-badge">{maxHighResChunks}</span>
                        </div>
                        <input
                            type="range"
                            className="premium-slider"
                            min="0"
                            max="64"
                            value={maxHighResChunks}
                            onInput={handleSliderChange}
                        />
                    </div>
                    <div className="switch-container">
                        <span className="switch-label">Region Inspector</span>
                        <label className="premium-switch">
                            <input
                                type="checkbox"
                                checked={inspectorMode}
                                onChange={handleInspectorToggleChange}
                            />
                            <span className="switch-slider"></span>
                        </label>
                    </div>
                    <div className="switch-container">
                        <span className="switch-label">Show Boundaries</span>
                        <label className="premium-switch">
                            <input
                                type="checkbox"
                                checked={showBoundaries}
                                onChange={handleBoundariesToggleChange}
                            />
                            <span className="switch-slider"></span>
                        </label>
                    </div>
                </div>
            </div>

            {/* Region Inspector Drawer Panel */}
            <RegionInspector
                activeRegion={activeRegion}
                sceneGraph={sceneGraph}
                onClose={handleCloseInspector}
                updateTick={updateTick}
            />
        </>
    );
}

interface RegionInspectorProps {
    activeRegion: { rx: number; rz: number } | null;
    sceneGraph: renderer.SceneGraph;
    onClose: () => void;
    updateTick: number;
}

function getWebGLTextureAsCanvas(
    gl: WebGL2RenderingContext,
    texture: WebGLTexture | null | undefined,
    width: number,
    height: number,
    isDepth: boolean
): HTMLCanvasElement | null {
    if (!texture) return null;

    try {
        const prevFb = gl.getParameter(gl.FRAMEBUFFER_BINDING);
        const prevTex = gl.getParameter(gl.TEXTURE_BINDING_2D);
        const prevAlignment = gl.getParameter(gl.PACK_ALIGNMENT);

        const fb = gl.createFramebuffer();
        gl.bindFramebuffer(gl.FRAMEBUFFER, fb);

        gl.bindTexture(gl.TEXTURE_2D, texture);
        gl.framebufferTexture2D(
            gl.FRAMEBUFFER,
            gl.COLOR_ATTACHMENT0,
            gl.TEXTURE_2D,
            texture,
            0
        );

        const status = gl.checkFramebufferStatus(gl.FRAMEBUFFER);
        if (status !== gl.FRAMEBUFFER_COMPLETE) {
            console.warn("Framebuffer not complete", status);
            gl.bindTexture(gl.TEXTURE_2D, prevTex);
            gl.bindFramebuffer(gl.FRAMEBUFFER, prevFb);
            gl.deleteFramebuffer(fb);
            return null;
        }

        gl.pixelStorei(gl.PACK_ALIGNMENT, 1);
        const pixels = new Uint8Array(width * height * 4);
        gl.readPixels(0, 0, width, height, gl.RGBA, gl.UNSIGNED_BYTE, pixels);

        // Restore gl states
        gl.pixelStorei(gl.PACK_ALIGNMENT, prevAlignment);
        gl.bindTexture(gl.TEXTURE_2D, prevTex);
        gl.bindFramebuffer(gl.FRAMEBUFFER, prevFb);
        gl.deleteFramebuffer(fb);

        if (isDepth) {
            for (let i = 0; i < pixels.length; i += 4) {
                pixels[i + 1] = pixels[i];
                pixels[i + 2] = pixels[i];
            }
        }

        const canvas = document.createElement("canvas");
        canvas.width = width;
        canvas.height = height;
        const ctx = canvas.getContext("2d");
        if (!ctx) return null;

        const imgData = new ImageData(new Uint8ClampedArray(pixels.buffer), width, height);
        ctx.putImageData(imgData, 0, 0);
        return canvas;
    } catch (e) {
        console.error("Error reading WebGL texture", e);
        return null;
    }
}

function CanvasRenderer({ canvas, className, style, onClick }: { canvas: HTMLCanvasElement; className?: string; style?: any; onClick?: () => void }) {
    const containerRef = useRef<HTMLDivElement>(null);
    useEffect(() => {
        const container = containerRef.current;
        if (!container) return;
        container.innerHTML = '';
        if (className) {
            canvas.className = className;
        }
        if (style) {
            Object.assign(canvas.style, style);
        }
        container.appendChild(canvas);
    }, [canvas, className, style]);

    return (
        <div
            ref={containerRef}
            onClick={onClick}
            style={{ display: 'block', cursor: onClick ? 'pointer' : 'default' }}
        />
    );
}

function RegionInspector({ activeRegion, sceneGraph, onClose, updateTick }: RegionInspectorProps) {
    const [loading, setLoading] = useState<boolean>(true);
    const [error, setError] = useState<boolean>(false);
    const [canvases, setCanvases] = useState<{ [type: number]: HTMLCanvasElement } | null>(null);
    const [zoomedImage, setZoomedImage] = useState<{ canvas: HTMLCanvasElement; title: string; width: number; height: number } | null>(null);

    // Reset zoom state when region changes
    useEffect(() => {
        setZoomedImage(null);
    }, [activeRegion]);

    // Side images LOD binary fetch and memory cleanup effect
    useEffect(() => {
        if (!activeRegion) return;

        const { rx, rz } = activeRegion;
        const region = sceneGraph.regions.get(`${rx},${rz}`);

        if (!region || region.impostor.status !== 'READY' || !region.impostor.textures) {
            setLoading(region ? (region.impostor.status === 'FETCH') : false);
            setError(region ? (region.impostor.status === 'ERROR') : false);
            setCanvases(null);
            return;
        }

        try {
            const gl = sceneGraph.context.gl as WebGL2RenderingContext;
            const texs = region.impostor.textures;
            const generatedCanvases: { [type: number]: HTMLCanvasElement } = {};

            const textureConfigs: [number, WebGLTexture | null | undefined, number, number, boolean][] = [
                [0, texs.texTop, 256, 256, true],       // topDepth
                [1, texs.texNorthColor, 256, 160, false], // side views
                [2, texs.texNorth, 256, 160, true],
                [3, texs.texSouthColor, 256, 160, false],
                [4, texs.texSouth, 256, 160, true],
                [5, texs.texEastColor, 256, 160, false],
                [6, texs.texEast, 256, 160, true],
                [7, texs.texWestColor, 256, 160, false],
                [8, texs.texWest, 256, 160, true],
                [99, texs.texTopColor, 256, 256, false],  // topColor
            ];

            for (const [id, tex, w, h, isDepth] of textureConfigs) {
                const canvas = getWebGLTextureAsCanvas(gl, tex, w, h, isDepth);
                if (canvas) generatedCanvases[id] = canvas;
            }

            setCanvases(generatedCanvases);
            setLoading(false);
        } catch (err) {
            console.error("Failed to read WebGL textures", err);
            setError(true);
            setLoading(false);
        }
    }, [activeRegion, sceneGraph, updateTick]);

    const handleCanvasClick = (canvas: HTMLCanvasElement, title: string) => {
        const nw = canvas.width;
        const nh = canvas.height;

        // Calculate max scale that fits the viewpoint
        const maxScaleX = Math.floor((window.innerWidth * 0.9) / nw);
        const maxScaleY = Math.floor((window.innerHeight * 0.9) / nh);
        let scale = Math.min(maxScaleX, maxScaleY);

        // Clamp scale to be between 2 and 3 if possible, otherwise at least 1
        scale = Math.max(1, Math.min(3, scale));

        // Create a clone of the canvas for the lightbox so it's a separate element in the DOM
        const clone = document.createElement("canvas");
        clone.width = nw;
        clone.height = nh;
        const ctx = clone.getContext("2d");
        if (ctx) {
            ctx.drawImage(canvas, 0, 0);
        }

        setZoomedImage({
            canvas: clone,
            title,
            width: nw * scale,
            height: nh * scale
        });
    };

    const active = !!activeRegion;
    const { rx, rz } = activeRegion || { rx: 0, rz: 0 };
    const region = activeRegion ? sceneGraph.regions.get(`${rx},${rz}`) : null;
    const impostorStatus = region ? region.impostor.status : 'NONE';

    // Render a region view card (Color and Depth side-by-side)
    const renderViewCard = (name: string, colorCanvas: HTMLCanvasElement | undefined, depthCanvas: HTMLCanvasElement | undefined, isTop: boolean = false) => {
        const height = isTop ? '256px' : '160px';
        return (
            <div className="side-card" key={name}>
                <div className="side-card-title">{name} View</div>
                <div className="side-images-row">
                    {colorCanvas ? (
                        <div className="side-img-box">
                            <div className="img-type-label">Color</div>
                            <CanvasRenderer
                                canvas={colorCanvas}
                                onClick={() => handleCanvasClick(colorCanvas, `${name} Color`)}
                                style={{ width: '256px', height: height, imageRendering: 'pixelated', display: 'block' }}
                            />
                        </div>
                    ) : (
                        <div className="side-img-box empty">
                            <div className="img-type-label">Color</div>
                            <div className="no-img-text">N/A</div>
                        </div>
                    )}
                    {depthCanvas ? (
                        <div className="side-img-box">
                            <div className="img-type-label">Depth</div>
                            <CanvasRenderer
                                canvas={depthCanvas}
                                onClick={() => handleCanvasClick(depthCanvas, `${name} Depth`)}
                                style={{ width: '256px', height: height, imageRendering: 'pixelated', filter: 'brightness(1.25)', display: 'block' }}
                            />
                        </div>
                    ) : (
                        <div className="side-img-box empty">
                            <div className="img-type-label">Depth</div>
                            <div className="no-img-text">N/A</div>
                        </div>
                    )}
                </div>
            </div>
        );
    };

    return (
        <>
            <div id="region-inspector" className={active ? 'active' : ''}>
                <div className="inspector-header">
                    <span className="inspector-title">REGION INSPECTOR</span>
                    <button className="inspector-close-btn" title="Close" onClick={onClose}>
                        &times;
                    </button>
                </div>
                {active && (
                    <div className="inspector-content">
                        {/* Meta Coords card */}
                        <div className="region-meta-card">
                            <div className="meta-coord">Region r.{rx}.{rz}</div>
                            <div className="meta-world">World X: [{rx * 512} to {(rx + 1) * 512}]</div>
                            <div className="meta-world">World Z: [{rz * 512} to {(rz + 1) * 512}]</div>
                        </div>

                        {/* Regionlets listing */}
                        {region ? (
                            <>
                                <div className="inspector-section-title">Regionlets (High-Res)</div>
                                <div className="regionlets-grid">
                                    {region.regionlets.map((rlet, idx) => {
                                        let totalCubes = 0;
                                        const layers = rlet.chunk?.layers || {};
                                        const layerItems = Object.entries(layers)
                                            .filter(([_, layerAttrib]) => layerAttrib && layerAttrib.size > 0)
                                            .map(([layerName, layerAttrib]) => {
                                                totalCubes += layerAttrib.size;
                                                return (
                                                    <div className="layer-item" key={layerName}>
                                                        <span className="layer-name">{layerName}</span>
                                                        <span className="layer-count">{layerAttrib.size.toLocaleString()}</span>
                                                    </div>
                                                );
                                            });

                                        const statusClass = `status-${rlet.status.toLowerCase()}`;
                                        return (
                                            <div className="regionlet-card" key={idx}>
                                                <div className="regionlet-header">
                                                    <span className="regionlet-index">Offset {rlet.off}</span>
                                                    <span className={`regionlet-status ${statusClass}`}>{rlet.status}</span>
                                                </div>
                                                <div className="regionlet-body">
                                                    <div className="meta-row">
                                                        <span>Y Bounds:</span>
                                                        <span>{rlet.chunk ? `${rlet.chunk.minY} - ${rlet.chunk.maxY}` : 'N/A'}</span>
                                                    </div>
                                                    <div className="meta-row">
                                                        <span>Blocks:</span>
                                                        <span style={{ fontWeight: 700, color: 'var(--accent)' }}>
                                                            {totalCubes.toLocaleString()}
                                                        </span>
                                                    </div>
                                                    <div className="layers-list">
                                                        {layerItems.length > 0 ? layerItems : (
                                                            <div className="no-layers">No voxels loaded</div>
                                                        )}
                                                    </div>
                                                </div>
                                            </div>
                                        );
                                    })}
                                </div>
                            </>
                        ) : (
                            <>
                                <div className="inspector-section-title">Regionlets (High-Res)</div>
                                <div className="region-meta-card" style={{ textAlign: 'center', fontStyle: 'italic', color: 'var(--text-sub)' }}>
                                    Region not instantiated in SceneGraph
                                </div>
                            </>
                        )}

                        {/* LOD */}
                        <div className="inspector-section-title">Level of Detail
                            <span className={`regionlet-status status-${region ? region.impostor.status.toLowerCase() : 'none'}`}>
                                {region ? region.impostor.status : 'NONE'}
                            </span>
                        </div>
                        {loading && (
                            <div id="lod-bin-loading" className="lod-loading">
                                <div className="spinner"></div>
                                <span>
                                    {impostorStatus === 'FETCH'
                                        ? "Fetching region impostor from server..."
                                        : "Reading GPU texture memory..."}
                                </span>
                            </div>
                        )}
                        {error && (
                            <div id="lod-bin-error" className="image-error" style={{ marginTop: '8px' }}>
                                Region impostor textures not loaded in GPU
                            </div>
                        )}
                        {!loading && !error && canvases && (
                            <div className="sides-gallery">
                                {renderViewCard('Top', canvases[99], canvases[0], true)}
                                {renderViewCard('North', canvases[1], canvases[2])}
                                {renderViewCard('South', canvases[3], canvases[4])}
                                {renderViewCard('East', canvases[5], canvases[6])}
                                {renderViewCard('West', canvases[7], canvases[8])}
                            </div>
                        )}
                    </div>
                )}
            </div>

            {/* Image zoom lightbox overlay */}
            {zoomedImage && (
                <div
                    id="image-lightbox"
                    onClick={() => setZoomedImage(null)}
                    style={{
                        position: 'fixed',
                        top: 0,
                        left: 0,
                        right: 0,
                        bottom: 0,
                        background: 'rgba(5, 5, 8, 0.92)',
                        zIndex: 9999,
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        cursor: 'pointer'
                    }}
                >
                    <div
                        className="lightbox-content"
                        style={{
                            display: 'flex',
                            flexDirection: 'column',
                            alignItems: 'center',
                            gap: '12px',
                            background: 'var(--bg-glass)',
                            border: '1px solid var(--border-glass)',
                            borderRadius: '8px',
                            padding: '16px',
                            boxShadow: '0 8px 32px rgba(0,0,0,0.6)'
                        }}
                    >
                        <div
                            className="lightbox-title"
                            style={{
                                color: 'var(--text-main)',
                                fontSize: '13px',
                                fontWeight: 700,
                                letterSpacing: '0.5px',
                                textTransform: 'uppercase'
                            }}
                        >
                            {zoomedImage.title}
                        </div>
                        <CanvasRenderer
                            canvas={zoomedImage.canvas}
                            className="lightbox-image"
                            style={{
                                width: `${zoomedImage.width}px`,
                                height: `${zoomedImage.height}px`,
                                imageRendering: 'pixelated',
                                display: 'block',
                                filter: zoomedImage.title.toLowerCase().includes('depth') ? 'brightness(1.25)' : 'none'
                            }}
                        />
                    </div>
                </div>
            )}
        </>
    );
}
