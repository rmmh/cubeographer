#version 300 es
precision highp float;

in vec3 position;

uniform mat4 modelViewMatrix;
uniform mat4 projectionMatrix;
uniform vec3 uCameraPosition;
uniform vec3 uGroupOffset;
uniform float uGroupSize; // G (1.0 to 8.0)

out vec3 vLocalPos;
out vec3 vLocalCam;
out vec3 vWorldPos;

void main() {
    // position goes from [0, 0, 0] to [512, 320, 512]
    // We scale x and z by uGroupSize
    vec3 scaledPos = position;
    scaledPos.x = position.x * uGroupSize;
    scaledPos.z = position.z * uGroupSize;

    // local position goes from [0, 0, 0] to [1, 1, 1] for the group
    vLocalPos = scaledPos / vec3(512.0 * uGroupSize, 320.0, 512.0 * uGroupSize);

    // camera position in local coordinates of the group
    vLocalCam = (uCameraPosition - uGroupOffset) / vec3(512.0 * uGroupSize, 320.0, 512.0 * uGroupSize);

    // World position of the vertex
    vWorldPos = scaledPos + uGroupOffset;

    gl_Position = projectionMatrix * modelViewMatrix * vec4(scaledPos, 1.0);
}
