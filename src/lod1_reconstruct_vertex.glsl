#version 300 es
precision highp float;

in vec4 a_vertexData;

uniform mat4 modelViewMatrix;
uniform mat4 projectionMatrix;
uniform vec3 uCameraPosition;
uniform vec3 uRegionOffset;
uniform float uMaxHeight;

out vec3 vLocalPos;
out vec3 vLocalCam;
flat out uint vNormalIndex;

void main() {
    vec3 position = a_vertexData.xyz;

    // Convert from voxel local [0..256, 0..160, 0..256] to region world [0..512, 0..320, 0..512]
    vec3 scaledPos;
    scaledPos.x = position.x * 2.0;
    scaledPos.y = position.y * 2.0;
    scaledPos.z = position.z * 2.0;

    vLocalPos = scaledPos / vec3(512.0, 320.0, 512.0);

    // Calculate camera position relative to the region
    vLocalCam = (uCameraPosition - uRegionOffset) / vec3(512.0, 320.0, 512.0);
    vNormalIndex = uint(a_vertexData.w);

    gl_Position = projectionMatrix * modelViewMatrix * vec4(scaledPos, 1.0);
}
