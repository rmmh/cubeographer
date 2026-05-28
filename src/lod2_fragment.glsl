#version 300 es
precision highp float;

in vec3 vLocalPos;
in vec3 vLocalCam;
in vec3 vWorldPos;

uniform sampler2D uColorTex;
uniform sampler2D uDepthTex;

uniform mat4 uVPInit; // Initial View-Projection matrix used to render FBO
uniform mat4 uVPCurrent; // Current View-Projection matrix
uniform vec3 uScale;  // Scale of the group (512 * G, 320, 512 * G)
uniform vec3 uOffset; // Group world offset
uniform float uFogScale; // Scale factor for fog based on camera height

out vec4 fragColor;

void main() {
    vec3 rayDir = normalize(vLocalPos - vLocalCam);

    // Find intersection of the ray with the bounding box bounds to know where to stop
    vec3 t0 = (vec3(0.0) - vLocalPos) / rayDir;
    vec3 t1 = (vec3(1.0) - vLocalPos) / rayDir;
    vec3 tMax = max(t0, t1);
    float t_exit = min(min(tMax.x, tMax.y), tMax.z);

    bool hit = false;
    vec3 hitLocalPos = vLocalPos;
    vec4 finalColor = vec4(0.0);
    float hitZ = 0.0;
    float hitW = 0.0;

    // Simple Parallax Mapping (One-step offset mapping)
    // 1. Project the entry position to get the initial uv
    vec3 worldP = vLocalPos * uScale + uOffset;
    vec4 clipP = uVPInit * vec4(worldP, 1.0);

    if (clipP.w > 0.0001) {
        vec3 ndcP = clipP.xyz / clipP.w;
        vec2 uv = ndcP.xy * 0.5 + 0.5;

        if (uv.x >= 0.0 && uv.x <= 1.0 && uv.y >= 0.0 && uv.y <= 1.0) {
            float z_tex = texture(uDepthTex, uv).r;

            if (z_tex > 0.0001) {
                // Correct for 16-bit depth buffer precision loss
                z_tex = min(1.0, z_tex + 0.5 / 65535.0);
                float z_front = ndcP.z * 0.5 + 0.5;

                // 2. Project the exit position to find the depth at the back
                vec3 backLocalP = vLocalPos + t_exit * rayDir;
                vec3 backWorldP = backLocalP * uScale + uOffset;
                vec4 backClipP = uVPInit * vec4(backWorldP, 1.0);
                float z_back = 0.0;
                if (backClipP.w > 0.0001) {
                    z_back = (backClipP.z / backClipP.w) * 0.5 + 0.5;
                }

                // 3. Linearly approximate `t` where the ray's depth equals the geometry depth `z_tex`
                float t = 0.0;
                if (abs(z_front - z_back) > 0.0001) {
                    t = t_exit * clamp((z_tex - z_front) / (z_back - z_front), 0.0, 1.0);
                }

                // 4. Sample color and compute final depth at the offset position
                hitLocalPos = vLocalPos + t * rayDir;
                vec3 finalWorldP = hitLocalPos * uScale + uOffset;
                vec4 finalClipP = uVPInit * vec4(finalWorldP, 1.0);

                if (finalClipP.w > 0.0001) {
                    vec2 finalUv = (finalClipP.xy / finalClipP.w) * 0.5 + 0.5;
                    if (finalUv.x >= 0.0 && finalUv.x <= 1.0 && finalUv.y >= 0.0 && finalUv.y <= 1.0) {
                        hit = true;
                        finalColor = texture(uColorTex, finalUv);

                        vec4 currentClipP = uVPCurrent * vec4(finalWorldP, 1.0);
                        hitZ = (currentClipP.z / currentClipP.w) * 0.5 + 0.5;
                        hitW = currentClipP.w;
                    }
                }
            }
        }
    }

    if (!hit || finalColor.a < 0.1) {
        discard;
    }

    gl_FragDepth = hitZ;

    // Apply fog based on current camera depth
    float fogFactor = min(1.0, hitW / 3000.0) * uFogScale;
    vec3 fogColor = vec3(0.722, 0.855, 1.0);
    fragColor = vec4(mix(finalColor.rgb, fogColor, fogFactor), finalColor.a);
}
