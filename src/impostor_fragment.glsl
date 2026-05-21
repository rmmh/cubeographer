#version 300 es
precision highp float;
precision highp int;

in vec3 vLocalPos;
in vec3 vLocalCam;

uniform sampler2D texTop;
uniform sampler2D texNorth;
uniform sampler2D texSouth;
uniform sampler2D texEast;
uniform sampler2D texWest;

uniform sampler2D texTopColor;
uniform sampler2D texNorthColor;
uniform sampler2D texSouthColor;
uniform sampler2D texEastColor;
uniform sampler2D texWestColor;

uniform mat4 projectionMatrix;
uniform mat4 modelViewMatrix;

out vec4 outColor;

// One 8-bit step: bridges the quantization gap at region boundaries where the
// encoded depth (capped at 254/255) just barely fails the boundary comparison.
const float DEPTH_BIAS = 1.0 / 255.0;

bool isSolid(vec3 p) {
    if (p.x < 0.0 || p.x > 1.0 || p.y < 0.0 || p.y > 1.0 || p.z < 0.0 || p.z > 1.0)
        return false;

    float top_depth   = texture(texTop,   vec2(p.x,        p.z)).r;
    float north_depth = texture(texNorth, vec2(p.x, 1.0 - p.y)).r;
    float south_depth = texture(texSouth, vec2(p.x, 1.0 - p.y)).r;
    float east_depth  = texture(texEast,  vec2(p.z, 1.0 - p.y)).r;
    float west_depth  = texture(texWest,  vec2(p.z, 1.0 - p.y)).r;

    bool solid_top   = (top_depth   < 0.999) && (p.y <= top_depth   + DEPTH_BIAS);
    bool solid_north = (north_depth < 0.999) && (p.z <= north_depth + DEPTH_BIAS);
    bool solid_south = (south_depth < 0.999) && (p.z >= south_depth - DEPTH_BIAS);
    bool solid_east  = (east_depth  < 0.999) && (p.x <= east_depth  + DEPTH_BIAS);
    bool solid_west  = (west_depth  < 0.999) && (p.x >= west_depth  - DEPTH_BIAS);

    return solid_top && solid_north && solid_south && solid_east && solid_west;
}

void main() {
    vec3 rayOrigin = vLocalCam;
    vec3 rayDir = normalize(vLocalPos - vLocalCam);

    // IEEE 754 division by zero produces ±infinity, which is correct for slab intersection.
    vec3 t0 = (vec3(0.0) - rayOrigin) / rayDir;
    vec3 t1 = (vec3(1.0) - rayOrigin) / rayDir;
    vec3 tMin = min(t0, t1);
    vec3 tMax = max(t0, t1);

    float t_enter = max(max(tMin.x, tMin.y), tMin.z);
    float t_exit  = min(min(tMax.x, tMax.y), tMax.z);
    t_enter = max(t_enter, 0.0);

    if (t_enter > t_exit) discard;
    if (isSolid(rayOrigin)) discard;

    const float stepSize = 0.003;
    bool hit = false;
    vec3 hitPos = vec3(0.0);
    float t_last_miss = t_enter;

    for (float t = t_enter; t < t_exit; t += stepSize) {
        vec3 p = rayOrigin + t * rayDir;
        if (isSolid(p)) {
            // Binary search between last miss and first hit to reduce stair-stepping.
            float t_lo = t_last_miss;
            float t_hi = t;
            for (int i = 0; i < 6; i++) {
                float t_mid = (t_lo + t_hi) * 0.5;
                if (isSolid(rayOrigin + t_mid * rayDir))
                    t_hi = t_mid;
                else
                    t_lo = t_mid;
            }
            hitPos = rayOrigin + t_hi * rayDir;
            hit = true;
            break;
        }
        t_last_miss = t;
    }

    if (!hit) discard;

    vec2 uv_top   = vec2(hitPos.x,        hitPos.z);
    vec2 uv_north = vec2(hitPos.x, 1.0 - hitPos.y);
    vec2 uv_south = vec2(hitPos.x, 1.0 - hitPos.y);
    vec2 uv_east  = vec2(hitPos.z, 1.0 - hitPos.y);
    vec2 uv_west  = vec2(hitPos.z, 1.0 - hitPos.y);

    float d_top   = abs(hitPos.y - texture(texTop,   uv_top).r);
    float d_north = abs(hitPos.z - texture(texNorth, uv_north).r);
    float d_south = abs(hitPos.z - texture(texSouth, uv_south).r);
    float d_east  = abs(hitPos.x - texture(texEast,  uv_east).r);
    float d_west  = abs(hitPos.x - texture(texWest,  uv_west).r);

    float min_d = min(min(min(min(d_top, d_north), d_south), d_east), d_west);

    vec3 voxelColor = vec3(0.0);
    vec3 normal = vec3(0.0, 1.0, 0.0);

    if (min_d == d_top) {
        voxelColor = texture(texTopColor, uv_top).rgb;
        normal = vec3(0.0, 1.0, 0.0);
    } else if (min_d == d_north) {
        voxelColor = texture(texNorthColor, uv_north).rgb;
        normal = vec3(0.0, 0.0, 1.0);
    } else if (min_d == d_south) {
        voxelColor = texture(texSouthColor, uv_south).rgb;
        normal = vec3(0.0, 0.0, -1.0);
    } else if (min_d == d_east) {
        voxelColor = texture(texEastColor, uv_east).rgb;
        normal = vec3(1.0, 0.0, 0.0);
    } else {
        voxelColor = texture(texWestColor, uv_west).rgb;
        normal = vec3(-1.0, 0.0, 0.0);
    }

    float diff = max(0.5, dot(vec3(abs(normal.x), normal.y, abs(normal.z)), vec3(0.6, 1.0, 0.8)));

    // Write depth at the raymarched hit point so adjacent impostors occlude correctly.
    vec4 clipHit = projectionMatrix * modelViewMatrix * vec4(hitPos * vec3(512.0, 320.0, 512.0), 1.0);
    gl_FragDepth = clipHit.z / clipHit.w * 0.5 + 0.5;

    outColor = mix(
        vec4(voxelColor * diff, 1.0),
        vec4(0.722, 0.855, 1.0, 1.0),
        min(1.0, clipHit.w / 3000.0)
    );
}
