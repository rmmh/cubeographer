#version 300 es
precision highp float;

in vec3 position;

uniform mat4 modelViewMatrix;
uniform mat4 projectionMatrix;
uniform vec3 uCameraPosition;
uniform vec3 uRegionOffset;
uniform float uMaxHeight;

out vec3 vLocalPos;
out vec3 vLocalCam;

void main() {
    // position goes from [0, 0, 0] to [512, 320, 512]
    // We scale the y position from [0, 320] to [0, uMaxHeight]
    vec3 scaledPos = position;
    scaledPos.y = position.y * (uMaxHeight / 320.0);

    vLocalPos = scaledPos / vec3(512.0, 320.0, 512.0);

    // Calculate camera position relative to the region
    vLocalCam = (uCameraPosition - uRegionOffset) / vec3(512.0, 320.0, 512.0);

    gl_Position = projectionMatrix * modelViewMatrix * vec4(scaledPos, 1.0);
}
