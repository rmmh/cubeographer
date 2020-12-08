/*
Copyright © 2010-2020 three.js authors

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/

// ported from https://github.com/mrdoob/three.js/blob/dev/examples/jsm/controls/OrbitControls.js

// This set of controls performs orbiting, dollying (zooming), and panning.
// Unlike TrackballControls, it maintains the "up" direction object.up (+Y by default).
//
//    Orbit - left mouse / touch: one-finger move
//    Zoom - middle mouse, or mousewheel / touch: two-finger spread or squish
//    Pan - right mouse, or left mouse + ctrl/meta/shiftKey, or arrow keys / touch: two-finger move
import { glMatrix, mat4, vec2, vec3, vec4, quat } from 'gl-matrix';
const MOUSE = { LEFT: 0, MIDDLE: 1, RIGHT: 2, ROTATE: 0, DOLLY: 1, PAN: 2 };
const TOUCH = { ROTATE: 0, PAN: 1, DOLLY_PAN: 2, DOLLY_ROTATE: 3 };
const STATE = {
    NONE: - 1,
    ROTATE: 0,
    DOLLY: 1,
    PAN: 2,
    TOUCH_ROTATE: 3,
    TOUCH_PAN: 4,
    TOUCH_DOLLY_PAN: 5,
    TOUCH_DOLLY_ROTATE: 6
};
const TWOPI = 2 * Math.PI;
/**
 * https://github.com/mrdoob/eventdispatcher.js/
 */


class EventDispatcher {
    _listeners: {[name: string]: any};

    constructor() {
        this._listeners = {};
    }

    addEventListener(type: string, listener: any) {
        const listeners = this._listeners;
        if (listeners[type] === undefined) {
            listeners[type] = [];
        }
        if (listeners[type].indexOf(listener) === - 1) {
            listeners[type].push(listener);
        }
    }

    hasEventListener(type: string, listener: any) {
        const listeners = this._listeners;
        return listeners[type] !== undefined && listeners[type].indexOf(listener) !== - 1;
    }

    removeEventListener(type: string, listener: any) {
        const listeners = this._listeners;
        const listenerArray = listeners[type];

        if (listenerArray !== undefined) {
            const index = listenerArray.indexOf(listener);
            if (index !== - 1) {
                listenerArray.splice(index, 1);
            }
        }
    }

    dispatchEvent(event: any) {
        const listeners = this._listeners;
        const listenerArray = listeners[event.type];
        if (listenerArray !== undefined) {
            event.target = this;
            // Make a copy, in case listeners are removed while iterating.
            const array = listenerArray.slice(0);
            for (let i = 0, l = array.length; i < l; i++) {
                array[i].call(this, event);
            }
        }
    }
}

class OrbitControls extends EventDispatcher {
    object: any
    domElement: any
    enabled = true // Set to false to disable control
    target: vec3
    minDistance = 0;
    maxDistance = Infinity;
    // How far you can zoom in and out ( OrthographicCamera only )
    minZoom = 0;
    maxZoom = Infinity;
    // How far you can orbit vertically, upper and lower limits.
    // Range is 0 to Math.PI radians.
    minPolarAngle = 0; // radians
    maxPolarAngle = Math.PI; // radians
    // How far you can orbit horizontally, upper and lower limits.
    // If set, the interval [ min, max ] must be a sub-interval of [ - 2 PI, 2 PI ], with ( max - min < 2 PI )
    minAzimuthAngle = - Infinity; // radians
    maxAzimuthAngle = Infinity; // radians
    // Set to true to enable damping (inertia)
    // If damping is enabled, you must call controls.update() in your animation loop
    enableDamping = false;
    dampingFactor = 0.05;
    // This option actually enables dollying in and out; left as "zoom" for backwards compatibility.
    // Set to false to disable zooming
    enableZoom = true;
    zoomSpeed = 1.0;
    // Set to false to disable rotating
    enableRotate = true;
    rotateSpeed = 1.0;
    // Set to false to disable panning
    enablePan = true;
    panSpeed = 1.0;
    screenSpacePanning = true; // if false, pan orthogonal to world-space direction camera.up
    keyPanSpeed = 7.0;	// pixels moved per arrow key push
    // Set to true to automatically rotate around the target
    // If auto-rotate is enabled, you must call controls.update() in your animation loop
    autoRotate = false;
    autoRotateSpeed = 2.0; // 30 seconds per round when fps is 60
    // Set to false to disable use of the keys
    enableKeys = true;
    // Mouse buttons
    mouseButtons = { LEFT: MOUSE.ROTATE, MIDDLE: MOUSE.DOLLY, RIGHT: MOUSE.PAN };
    // Touch fingers
    touches = { ONE: TOUCH.ROTATE, TWO: TOUCH.DOLLY_PAN };

    // for reset
    target0: vec3
    position0: vec3
    zoom0: number

    boundPointerMove: (evt: PointerEvent) => void;
    boundPointerUp: (evt: PointerEvent) => void;

    constructor(object: any, domElement: any) {
        super();
        if (domElement === undefined) console.warn('THREE.OrbitControls: The second parameter "domElement" is now mandatory.');
        if (domElement === document) console.error('THREE.OrbitControls: "document" should not be used as the target "domElement". Please use "renderer.domElement" instead.');
        this.object = object;
        this.domElement = domElement;

        this.target = vec3.create();

        // for reset
        this.target0 = vec3.copy(vec3.create(), this.target);
        this.position0 = vec3.copy(vec3.create(), this.object.position);
        this.zoom0 = this.object.zoom;

        this.domElement.addEventListener('contextmenu', this.onContextMenu.bind(this), false);
        this.domElement.addEventListener('pointerdown', this.onPointerDown.bind(this), false);
        this.domElement.addEventListener('wheel', this.onMouseWheel.bind(this), false);
        this.domElement.addEventListener('touchstart', this.onTouchStart.bind(this), false);
        this.domElement.addEventListener('touchend', this.onTouchEnd.bind(this), false);
        this.domElement.addEventListener('touchmove', this.onTouchMove.bind(this), false);
        this.domElement.ownerDocument.addEventListener('keydown', this.onKeyDown.bind(this), false);

        this.boundPointerMove = this.onPointerMove.bind(this);
        this.boundPointerUp = this.onPointerUp.bind(this);

        // force an update at start
        this.update();
    }

    //
    // public methods
    //
    getPolarAngle() {
        return this.spherical.phi;
    }
    getAzimuthalAngle() {
        return this.spherical.theta;
    }
    saveState() {
        vec3.copy(this.target0, this.target);
        vec3.copy(this.position0, this.object.position);
        this.zoom0 = this.object.zoom;
    }
    reset() {
        vec3.copy(this.target, this.target0);
        this.object.position.copy(this.position0);
        this.object.zoom = this.zoom0;
        this.object.updateProjectionMatrix();
        this.dispatchEvent(this.changeEvent);
        this.update();
        this.state = STATE.NONE;
    }

    // this method is exposed, but perhaps it would be better if we can make it private...


    private offset = vec3.create();
    // so camera.up is the orbit axis
    private rot = quat.fromEuler(quat.create(), 0, 1, 0);
    private quatInverse = quat.invert(quat.create(), this.rot);
    private lastPosition = vec3.create();
    private lastQuaternion = quat.create();

    update() {
        var position: vec3 = this.object.position;
        vec3.sub(this.offset, position, this.target);
        // rotate offset to "y-axis-is-up" space
        vec3.transformQuat(this.offset, this.offset, this.rot);

        // angle from z-axis around y-axis
        this.spherical.setFromVector3(this.offset);

        if (this.autoRotate && this.state === STATE.NONE) {
            this.rotateLeft(this.getAutoRotationAngle());
        }
        if (this.enableDamping) {
            this.spherical.theta += this.sphericalDelta.theta * this.dampingFactor;
            this.spherical.phi += this.sphericalDelta.phi * this.dampingFactor;
        } else {
            this.spherical.theta += this.sphericalDelta.theta;
            this.spherical.phi += this.sphericalDelta.phi;
        }

        // restrict theta to be between desired limits
        var min = this.minAzimuthAngle;
        var max = this.maxAzimuthAngle;
        if (isFinite(min) && isFinite(max)) {
            if (min < - Math.PI) min += TWOPI; else if (min > Math.PI) min -= TWOPI;
            if (max < - Math.PI) max += TWOPI; else if (max > Math.PI) max -= TWOPI;
            if (min <= max) {
                this.spherical.theta = Math.max(min, Math.min(max, this.spherical.theta));
            } else {
                this.spherical.theta = (this.spherical.theta > (min + max) / 2) ?
                    Math.max(min, this.spherical.theta) :
                    Math.min(max, this.spherical.theta);
            }
        }
        // restrict phi to be between desired limits
        this.spherical.phi = Math.max(this.minPolarAngle, Math.min(this.maxPolarAngle, this.spherical.phi));
        this.spherical.makeSafe();

        this.spherical.radius *= this.scale;
        // restrict radius to be between desired limits
        this.spherical.radius = Math.max(this.minDistance, Math.min(this.maxDistance, this.spherical.radius));
        // move target to panned location
        if (this.enableDamping === true) {
            vec3.scaleAndAdd(this.target, this.target, this.panOffset, this.dampingFactor);
        } else {
            vec3.add(this.target, this.target, this.panOffset);
        }

        this.spherical.writeVec3(this.offset);
        // rotate offset back to "camera-up-vector-is-up" space
        vec3.transformQuat(this.offset, this.offset, this.quatInverse);

        vec3.add(position, this.target, this.offset);
        this.object.lookAt(this.target);
        if (this.enableDamping === true) {
            this.sphericalDelta.theta *= (1 - this.dampingFactor);
            this.sphericalDelta.phi *= (1 - this.dampingFactor);
            vec3.scale(this.panOffset, this.panOffset, 1 - this.dampingFactor);
        } else {
            this.sphericalDelta.set(0, 0, 0);
            vec3.set(this.panOffset, 0, 0, 0);
        }
        this.scale = 1;

        // update condition is:
        // min(camera displacement, camera rotation in radians)^2 > EPS
        // using small-angle approximation cos(x/2) = 1 - x^2 / 8
        if (this.zoomChanged ||
            vec3.sqrDist(this.lastPosition, this.object.position) > this.EPS ||
            quat.getAngle(this.lastQuaternion, this.object.quaternion) > this.EPS) {
            this.dispatchEvent(this.changeEvent);
            vec3.copy(this.lastPosition, this.object.position);
            quat.copy(this.lastQuaternion, this.object.quaternion);
            this.zoomChanged = false;
            this.object.update();
            return true;
        }
        return false;
    }

    dispose() {
        this.domElement.removeEventListener('contextmenu', this.onContextMenu, false);
        this.domElement.removeEventListener('pointerdown', this.onPointerDown, false);
        this.domElement.removeEventListener('wheel', this.onMouseWheel, false);
        this.domElement.removeEventListener('touchstart', this.onTouchStart, false);
        this.domElement.removeEventListener('touchend', this.onTouchEnd, false);
        this.domElement.removeEventListener('touchmove', this.onTouchMove, false);
        this.domElement.ownerDocument.removeEventListener('pointermove', this.boundPointerMove, false);
        this.domElement.ownerDocument.removeEventListener('pointerup', this.boundPointerUp, false);
        this.domElement.ownerDocument.removeEventListener('keydown', this.onKeyDown, false);
        //this.dispatchEvent( { type: 'dispose' } ); // should this be added here?
    }

    //
    // internals
    //
    private changeEvent = { type: 'change' };
    private startEvent = { type: 'start' };
    private endEvent = { type: 'end' };
    private state = STATE.NONE;
    private EPS = 0.000001;
    // current position in spherical coordinates
    private spherical = new Spherical();
    private sphericalDelta = new Spherical();
    private scale = 1;
    private panOffset = vec3.create();
    private zoomChanged = false;
    private rotateStart = vec2.create();
    private rotateEnd = vec2.create();
    private rotateDelta = vec2.create();
    private panStart = vec2.create();
    private panEnd = vec2.create();
    private panDelta = vec2.create();
    private dollyStart = vec2.create();
    private dollyEnd = vec2.create();
    private dollyDelta = vec2.create();

    private getAutoRotationAngle() {
        return 2 * Math.PI / 60 / 60 * this.autoRotateSpeed;
    }
    private getZoomScale() {
        return Math.pow(0.95, this.zoomSpeed);
    }
    private rotateLeft(angle: number) {
        this.sphericalDelta.theta -= angle;
    }
    private rotateUp(angle: number) {
        this.sphericalDelta.phi -= angle;
    }
    private panLeft(distance: number) {
        let forward = vec3.sub(vec3.create(), this.object.position, this.object.target);
        let right = vec3.cross(vec3.create(), forward, vec3.fromValues(0, 1, 0));
        // vec3.set(v, objectMatrix[0], objectMatrix[1], objectMatrix[2]);
        vec3.scale(right, right, distance * .5 * vec3.len(forward) / this.domElement.clientWidth);
        vec3.add(this.panOffset, this.panOffset, right);
    }
    private panUp(distance: number) {
        let forward = vec3.sub(vec3.create(), this.object.position, this.object.target);
        let right = vec3.cross(vec3.create(), forward, vec3.fromValues(0, 1, 0));
        // vec3.set(v, objectMatrix[0], objectMatrix[1], objectMatrix[2]);
        let v = vec3.cross(vec3.create(), forward, vec3.normalize(right, right));
        
        /*
        if (this.screenSpacePanning === true) {
            vec3.set(v, objectMatrix[4], objectMatrix[5], objectMatrix[6]);
        } else {
            vec3.set(v, objectMatrix[0], objectMatrix[1], objectMatrix[2]);
            vec3.cross(v, v, this.object.up);
        }
        */
        vec3.scale(v, v, -distance * .5 * vec3.len(forward) / this.domElement.clientHeight);
        vec3.add(this.panOffset, this.panOffset, v);
    }
    // deltaX and deltaY are in pixels; right and down are positive
    private pan = function () {
        var offset = vec3.create();
        return function(deltaX: number, deltaY: number) {
            var element = this.domElement;
            if (this.object.isPerspectiveCamera) {
                // perspective
                var position = this.object.position;
                vec3.sub(offset, position, this.target);
                var targetDistance = vec3.length(offset)
                // half of the fov is center to top of screen
                targetDistance *= Math.tan((this.object.fov / 2) * Math.PI / 180.0);
                // we use only clientHeight here so aspect ratio does not distort speed
                this.panLeft(2 * deltaX * targetDistance / element.clientHeight, this.object.matrix);
                this.panUp(2 * deltaY * targetDistance / element.clientHeight, this.object.matrix);
            } else if (this.object.isOrthographicCamera) {
                // orthographic
                this.panLeft(deltaX * (this.object.right - this.object.left) / this.object.zoom / element.clientWidth, this.object.matrix);
                this.panUp(deltaY * (this.object.top - this.object.bottom) / this.object.zoom / element.clientHeight, this.object.matrix);
            } else {
                // camera neither orthographic nor perspective
                console.warn('WARNING: OrbitControls.js encountered an unknown camera type - pan disabled.');
                this.enablePan = false;
            }
        };
    }();
    private dollyOut(dollyScale: number) {
        if (this.object.isPerspectiveCamera) {
            this.scale /= dollyScale;
        } else if (this.object.isOrthographicCamera) {
            this.object.zoom = Math.max(this.minZoom, Math.min(this.maxZoom, this.object.zoom * dollyScale));
            this.object.updateProjectionMatrix();
            this.zoomChanged = true;
        } else {
            console.warn('WARNING: OrbitControls.js encountered an unknown camera type - dolly/zoom disabled.');
            this.enableZoom = false;
        }
    }
    private dollyIn(dollyScale: number) {
        if (this.object.isPerspectiveCamera) {
            this.scale *= dollyScale;
        } else if (this.object.isOrthographicCamera) {
            this.object.zoom = Math.max(this.minZoom, Math.min(this.maxZoom, this.object.zoom / dollyScale));
            this.object.updateProjectionMatrix();
            this.zoomChanged = true;
        } else {
            console.warn('WARNING: OrbitControls.js encountered an unknown camera type - dolly/zoom disabled.');
            this.enableZoom = false;
        }
    }
    //
    // event callbacks - update the object state
    //
    private handleMouseDownRotate(event: MouseEvent) {
        vec2.set(this.rotateStart, event.clientX, event.clientY);
    }
    private handleMouseDownDolly(event: MouseEvent) {
        vec2.set(this.dollyStart, event.clientX, event.clientY);
    }
    private handleMouseDownPan(event: MouseEvent) {
        vec2.set(this.panStart, event.clientX, event.clientY);
    }
    private handleMouseMoveRotate(event: MouseEvent) {
        vec2.set(this.rotateEnd, event.clientX, event.clientY);
        vec2.sub(this.rotateDelta, this.rotateEnd, this.rotateStart)
        vec2.scale(this.rotateDelta, this.rotateDelta, this.rotateSpeed);
        var element = this.domElement;
        this.rotateLeft(2 * Math.PI * this.rotateDelta[0] / element.clientHeight); // yes, height
        this.rotateUp(2 * Math.PI * this.rotateDelta[1] / element.clientHeight);
        vec2.copy(this.rotateStart, this.rotateEnd);
        this.update();
    }
    private handleMouseMoveDolly(event: MouseEvent) {
        vec2.set(this.dollyEnd, event.clientX, event.clientY);
        vec2.sub(this.dollyDelta, this.dollyEnd, this.dollyStart);
        if (this.dollyDelta[1] > 0) {
            this.dollyOut(this.getZoomScale());
        } else if (this.dollyDelta[1] < 0) {
            this.dollyIn(this.getZoomScale());
        }
        vec2.copy(this.dollyStart, this.dollyEnd);
        this.update();
    }
    private handleMouseMovePan(event: MouseEvent) {
        vec2.set(this.panEnd, event.clientX, event.clientY);
        vec2.sub(this.panDelta, this.panEnd, this.panStart);
        vec2.scale(this.panDelta, this.panDelta, this.panSpeed);
        this.pan(this.panDelta[0], this.panDelta[1]);
        vec2.copy(this.panStart, this.panEnd);
        this.update();
    }
    private handleMouseUp(_event: MouseEvent) {
        // no-op
    }
    private handleMouseWheel(event: MouseWheelEvent) {
        if (event.deltaY < 0) {
            this.dollyIn(this.getZoomScale());
        } else if (event.deltaY > 0) {
            this.dollyOut(this.getZoomScale());
        }
        this.update();
    }
    private handleKeyDown(event: KeyboardEvent) {
        var needsUpdate = true;
        let dir = vec3.create();
        let rot = 0;
        switch (event.code) { // everything that supports WebGL2 supports event.code :)
            case "KeyR":
                dir[1] = 1;
                break;
            case "KeyF":
                dir[1] = -1;
                break;
            case "KeyA":
            case "ArrowLeft":
                rot = 1;
                break;
            case "KeyD":
            case "ArrowRight":
                rot = -1;
                break;
            case "KeyW":
            case "ArrowUp":
                dir[2] = -1;
                break;
            case "KeyS":
            case "ArrowDown":
                dir[2] = 1;
                break;
            case "KeyQ":
                dir[0] = 1;
                break;
            case "KeyE":
                dir[0] = -1;
                break;
            default:
                needsUpdate = false;
        }

        if (rot !== 0) {
            let forward = vec3.sub(vec3.create(), this.object.position, this.target);
            let forwardRot = vec3.rotateY(vec3.create(), forward, vec3.create(), rot * Math.PI / 12);
            vec3.sub(this.target, this.object.position, forwardRot);
        }

        if (vec3.length(dir) > 0) {
            let forward = vec3.sub(vec3.create(), this.object.position, this.object.target);
            forward[1] = 0;
            vec3.normalize(forward, forward);
            let right = vec3.cross(vec3.create(), forward, vec3.fromValues(0, 1, 0));

            let rotDir = vec3.scale(vec3.create(), forward, dir[2]);
            vec3.scaleAndAdd(rotDir, rotDir, right, dir[0]);
            rotDir[1] = dir[1];

            vec3.scale(rotDir, rotDir, this.keyPanSpeed);

            vec3.add(this.panOffset, this.panOffset, rotDir);
        }

        if (needsUpdate) {
            // prevent the browser from scrolling on cursor keys
            // if (event.code.startsWith("Arrow")) event.preventDefault();
            this.update();
        }
    }
    private handleTouchStartRotate(event: TouchEvent) {
        if (event.touches.length == 1) {
            vec2.set(this.rotateStart, event.touches[0].pageX, event.touches[0].pageY);
        } else {
            var x = 0.5 * (event.touches[0].pageX + event.touches[1].pageX);
            var y = 0.5 * (event.touches[0].pageY + event.touches[1].pageY);
            vec2.set(this.rotateStart, x, y);
        }
    }
    private handleTouchStartPan(event: TouchEvent) {
        if (event.touches.length == 1) {
            vec2.set(this.panStart, event.touches[0].pageX, event.touches[0].pageY);
        } else {
            var x = 0.5 * (event.touches[0].pageX + event.touches[1].pageX);
            var y = 0.5 * (event.touches[0].pageY + event.touches[1].pageY);
            vec2.set(this.panStart, x, y);
        }
    }
    private handleTouchStartDolly(event: TouchEvent) {
        var dx = event.touches[0].pageX - event.touches[1].pageX;
        var dy = event.touches[0].pageY - event.touches[1].pageY;
        var distance = Math.sqrt(dx * dx + dy * dy);
        vec2.set(this.dollyStart, 0, distance);
    }
    private handleTouchStartDollyPan(event: TouchEvent) {
        if (this.enableZoom) this.handleTouchStartDolly(event);
        if (this.enablePan) this.handleTouchStartPan(event);
    }
    private handleTouchStartDollyRotate(event: TouchEvent) {
        if (this.enableZoom) this.handleTouchStartDolly(event);
        if (this.enableRotate) this.handleTouchStartRotate(event);
    }
    private handleTouchMoveRotate(event: TouchEvent) {
        if (event.touches.length == 1) {
            vec2.set(this.rotateEnd, event.touches[0].pageX, event.touches[0].pageY);
        } else {
            var x = 0.5 * (event.touches[0].pageX + event.touches[1].pageX);
            var y = 0.5 * (event.touches[0].pageY + event.touches[1].pageY);
            vec2.set(this.rotateEnd, x, y);
        }
        vec2.sub(this.rotateDelta, this.rotateEnd, this.rotateStart)
        vec2.scale(this.rotateDelta, this.rotateDelta, this.rotateSpeed);
        var element = this.domElement;
        this.rotateLeft(2 * Math.PI * this.rotateDelta[0] / element.clientHeight); // yes, height
        this.rotateUp(2 * Math.PI * this.rotateDelta[0] / element.clientHeight);
        vec2.copy(this.rotateStart, this.rotateEnd);
    }
    private handleTouchMovePan(event: TouchEvent) {
        if (event.touches.length == 1) {
            vec2.set(this.panEnd, event.touches[0].pageX, event.touches[0].pageY);
        } else {
            var x = 0.5 * (event.touches[0].pageX + event.touches[1].pageX);
            var y = 0.5 * (event.touches[0].pageY + event.touches[1].pageY);
            vec2.set(this.panEnd, x, y);
        }
        vec2.sub(this.panDelta, this.panEnd, this.panStart)
        vec2.scale(this.panDelta, this.panDelta, this.panSpeed);
        this.pan(this.panDelta[0], this.panDelta[0]);
        vec2.copy(this.panStart, this.panEnd);
    }
    private handleTouchMoveDolly(event: TouchEvent) {
        var dx = event.touches[0].pageX - event.touches[1].pageX;
        var dy = event.touches[0].pageY - event.touches[1].pageY;
        var distance = Math.sqrt(dx * dx + dy * dy);
        vec2.set(this.dollyEnd, 0, distance);
        vec2.set(this.dollyDelta, 0, Math.pow(this.dollyEnd[0] / this.dollyStart[0], this.zoomSpeed));
        this.dollyOut(this.dollyDelta[0]);
        vec2.copy(this.dollyStart, this.dollyEnd);
    }
    private handleTouchMoveDollyPan(event: TouchEvent) {
        if (this.enableZoom) this.handleTouchMoveDolly(event);
        if (this.enablePan) this.handleTouchMovePan(event);
    }
    private handleTouchMoveDollyRotate(event: TouchEvent) {
        if (this.enableZoom) this.handleTouchMoveDolly(event);
        if (this.enableRotate) this.handleTouchMoveRotate(event);
    }
    private handleTouchEnd(_event: TouchEvent) {
        // no-op
    }
    //
    // event handlers - FSM: listen for events and reset state
    //
    private onPointerDown(event: PointerEvent) {
        if (this.enabled === false) return;
        switch (event.pointerType) {
            case 'mouse':
            case 'pen':
                this.onMouseDown(event);
                break;
            // TODO touch
        }
    }
    private onPointerMove(event: PointerEvent) {
        if (this.enabled === false) return;
        switch (event.pointerType) {
            case 'mouse':
            case 'pen':
                this.onMouseMove(event);
                break;
            // TODO touch
        }
    }
    private onPointerUp(event: PointerEvent) {
        if (this.enabled === false) return;
        switch (event.pointerType) {
            case 'mouse':
            case 'pen':
                this.onMouseUp(event);
                break;
            // TODO touch
        }
    }
    private onMouseDown(event: MouseEvent) {
        // Prevent the browser from scrolling.
        event.preventDefault();
        // Manually set the focus since calling preventDefault above
        // prevents the browser from setting it automatically.
        this.domElement.focus ? this.domElement.focus() : window.focus();
        var mouseAction;
        switch (event.button) {
            case 0:
                mouseAction = this.mouseButtons.LEFT;
                break;
            case 1:
                mouseAction = this.mouseButtons.MIDDLE;
                break;
            case 2:
                mouseAction = this.mouseButtons.RIGHT;
                break;
            default:
                mouseAction = - 1;
        }
        switch (mouseAction) {
            case MOUSE.DOLLY:
                if (this.enableZoom === false) return;
                this.handleMouseDownDolly(event);
                this.state = STATE.DOLLY;
                break;
            case MOUSE.ROTATE:
                if (event.ctrlKey || event.metaKey || event.shiftKey) {
                    if (this.enablePan === false) return;
                    this.handleMouseDownPan(event);
                    this.state = STATE.PAN;
                } else {
                    if (this.enableRotate === false) return;
                    this.handleMouseDownRotate(event);
                    this.state = STATE.ROTATE;
                }
                break;
            case MOUSE.PAN:
                if (event.ctrlKey || event.metaKey || event.shiftKey) {
                    if (this.enableRotate === false) return;
                    this.handleMouseDownRotate(event);
                    this.state = STATE.ROTATE;
                } else {
                    if (this.enablePan === false) return;
                    this.handleMouseDownPan(event);
                    this.state = STATE.PAN;
                }
                break;
            default:
                this.state = STATE.NONE;
        }
        if (this.state !== STATE.NONE) {
            this.domElement.ownerDocument.addEventListener('pointermove', this.boundPointerMove, false);
            this.domElement.ownerDocument.addEventListener('pointerup', this.boundPointerUp, false);
            this.dispatchEvent(this.startEvent);
        }
    }
    private onMouseMove(event: MouseEvent) {
        if (this.enabled === false) return;
        event.preventDefault();
        switch (this.state) {
            case STATE.ROTATE:
                if (this.enableRotate === false) return;
                this.handleMouseMoveRotate(event);
                break;
            case STATE.DOLLY:
                if (this.enableZoom === false) return;
                this.handleMouseMoveDolly(event);
                break;
            case STATE.PAN:
                if (this.enablePan === false) return;
                this.handleMouseMovePan(event);
                break;
        }
    }
    private onMouseUp(event: MouseEvent) {
        if (this.enabled === false) return;
        this.handleMouseUp(event);
        this.domElement.ownerDocument.removeEventListener('pointermove', this.boundPointerMove, false);
        this.domElement.ownerDocument.removeEventListener('pointerup', this.boundPointerUp, false);
        this.dispatchEvent(this.endEvent);
        this.state = STATE.NONE;
    }
    private onMouseWheel(event: WheelEvent) {
        if (this.enabled === false || this.enableZoom === false || (this.state !== STATE.NONE && this.state !== STATE.ROTATE)) return;
        event.preventDefault();
        event.stopPropagation();
        this.dispatchEvent(this.startEvent);
        this.handleMouseWheel(event);
        this.dispatchEvent(this.endEvent);
    }
    private onKeyDown(event: KeyboardEvent) {
        if (this.enabled === false || this.enableKeys === false || this.enablePan === false) return;
        this.handleKeyDown(event);
    }
    private onTouchStart(event: TouchEvent) {
        if (this.enabled === false) return;
        event.preventDefault(); // prevent scrolling
        switch (event.touches.length) {
            case 1:
                switch (this.touches.ONE) {
                    case TOUCH.ROTATE:
                        if (this.enableRotate === false) return;
                        this.handleTouchStartRotate(event);
                        this.state = STATE.TOUCH_ROTATE;
                        break;
                    case TOUCH.PAN:
                        if (this.enablePan === false) return;
                        this.handleTouchStartPan(event);
                        this.state = STATE.TOUCH_PAN;
                        break;
                    default:
                        this.state = STATE.NONE;
                }
                break;
            case 2:
                switch (this.touches.TWO) {
                    case TOUCH.DOLLY_PAN:
                        if (this.enableZoom === false && this.enablePan === false) return;
                        this.handleTouchStartDollyPan(event);
                        this.state = STATE.TOUCH_DOLLY_PAN;
                        break;
                    case TOUCH.DOLLY_ROTATE:
                        if (this.enableZoom === false && this.enableRotate === false) return;
                        this.handleTouchStartDollyRotate(event);
                        this.state = STATE.TOUCH_DOLLY_ROTATE;
                        break;
                    default:
                        this.state = STATE.NONE;
                }
                break;
            default:
                this.state = STATE.NONE;
        }
        if (this.state !== STATE.NONE) {
            this.dispatchEvent(this.startEvent);
        }
    }
    private onTouchMove(event: TouchEvent) {
        if (this.enabled === false) return;
        event.preventDefault(); // prevent scrolling
        event.stopPropagation();
        switch (this.state) {
            case STATE.TOUCH_ROTATE:
                if (this.enableRotate === false) return;
                this.handleTouchMoveRotate(event);
                this.update();
                break;
            case STATE.TOUCH_PAN:
                if (this.enablePan === false) return;
                this.handleTouchMovePan(event);
                this.update();
                break;
            case STATE.TOUCH_DOLLY_PAN:
                if (this.enableZoom === false && this.enablePan === false) return;
                this.handleTouchMoveDollyPan(event);
                this.update();
                break;
            case STATE.TOUCH_DOLLY_ROTATE:
                if (this.enableZoom === false && this.enableRotate === false) return;
                this.handleTouchMoveDollyRotate(event);
                this.update();
                break;
            default:
                this.state = STATE.NONE;
        }
    }
    private onTouchEnd(event: TouchEvent) {
        if (this.enabled === false) return;
        this.handleTouchEnd(event);
        this.dispatchEvent(this.endEvent);
        this.state = STATE.NONE;
    }
    private onContextMenu(event: TouchEvent) {
        if (this.enabled === false) return;
        event.preventDefault();
    }
}

// This set of controls performs orbiting, dollying (zooming), and panning.
// Unlike TrackballControls, it maintains the "up" direction object.up (+Y by default).
// This is very similar to OrbitControls, another set of touch behavior
//
//    Orbit - right mouse, or left mouse + ctrl/meta/shiftKey / touch: two-finger rotate
//    Zoom - middle mouse, or mousewheel / touch: two-finger spread or squish
//    Pan - left mouse, or arrow keys / touch: one-finger move
class MapControls extends OrbitControls {
    constructor(object: any, domElement: HTMLElement) {
        super(object, domElement);
        this.screenSpacePanning = false; // pan orthogonal to world-space direction camera.up
        this.mouseButtons.LEFT = MOUSE.PAN;
        this.mouseButtons.RIGHT = MOUSE.ROTATE;
        this.touches.ONE = TOUCH.PAN;
        this.touches.TWO = TOUCH.DOLLY_ROTATE;
    }
}

/**
 * Ref: https://en.wikipedia.org/wiki/Spherical_coordinate_system
 *
 * The polar angle (phi) is measured from the positive y-axis. The positive y-axis is up.
 * The azimuthal angle (theta) is measured from the positive z-axis.
 */
class Spherical {
    constructor(public radius = 1, public phi = 0, public theta = 0) { }
    set(radius: number, phi: number, theta: number) {
        this.radius = radius;
        this.phi = phi;
        this.theta = theta;
        return this;
    }
    clone() {
        return new Spherical().copy(this);
    }
    copy(other: Spherical) {
        this.radius = other.radius;
        this.phi = other.phi;
        this.theta = other.theta;
        return this;
    }
    // restrict phi to be betwee EPS and PI-EPS
    makeSafe() {
        const EPS = 0.000001;
        this.phi = Math.max(EPS, Math.min(Math.PI - EPS, this.phi));
        return this;
    }
    setFromVector3(v: vec3) {
        return this.setFromCartesianCoords(v[0], v[1], v[2]);
    }
    setFromCartesianCoords(x: number, y: number, z: number) {
        this.radius = Math.sqrt(x * x + y * y + z * z);
        if (this.radius === 0) {
            this.theta = 0;
            this.phi = 0;
        } else {
            this.theta = Math.atan2(x, z);
            this.phi = Math.acos(Math.min(1, Math.max(-1, y / this.radius)));
        }
        return this;
    }

    writeVec3(v: vec3) {
        return Spherical.setFromSphericalCoords(v, this.radius, this.phi, this.theta);
    }

    static setFromSphericalCoords(v: vec3, radius: number, phi: number, theta: number) {
        const sinPhiRadius = Math.sin(phi) * radius;

        return vec3.set(v,
            sinPhiRadius * Math.sin(theta),
            Math.cos(phi) * radius,
            sinPhiRadius * Math.cos(theta))
    }
}
export { OrbitControls, MapControls };
