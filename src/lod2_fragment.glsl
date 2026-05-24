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

    // Raymarching parallax loop
    const int NUM_STEPS = 16;
    float t_step = t_exit / float(NUM_STEPS);

    bool hit = false;
    vec3 hitLocalPos = vLocalPos;
    vec4 finalColor = vec4(0.0);
    float hitZ = 0.0;
    float hitW = 0.0;

    for (int i = 0; i <= NUM_STEPS; i++) {
        float t = float(i) * t_step;
        vec3 localP = vLocalPos + t * rayDir;
        vec3 worldP = localP * uScale + uOffset;

        // Project worldP to initial camera's NDC space
        vec4 clipP = uVPInit * vec4(worldP, 1.0);

        // Guard against points behind the FBO camera plane (w <= 0)
        if (clipP.w <= 0.0001) {
            continue;
        }

        vec3 ndcP = clipP.xyz / clipP.w;
        vec2 uv = ndcP.xy * 0.5 + 0.5;

        // If UV is outside the initial camera's frame, skip/break
        if (uv.x < 0.0 || uv.x > 1.0 || uv.y < 0.0 || uv.y > 1.0) {
            continue;
        }

        float z_ray = ndcP.z * 0.5 + 0.5;
        float z_tex = texture(uDepthTex, uv).r;

        // Inverse-Z check: larger Z is closer to the camera.
        // We hit the geometry when z_ray <= z_tex.
        if (z_ray <= z_tex && z_tex > 0.0001) {
            hit = true;
            hitLocalPos = localP;
            hitZ = z_ray;

            // Perform basic binary search for precision
            float t_prev = float(i - 1) * t_step;
            float t_curr = t;
            for (int k = 0; k < 4; k++) {
                float t_mid = (t_prev + t_curr) * 0.5;
                vec3 midLocalP = vLocalPos + t_mid * rayDir;
                vec3 midWorldP = midLocalP * uScale + uOffset;
                vec4 midClip = uVPInit * vec4(midWorldP, 1.0);

                // Guard against points behind the FBO camera plane
                if (midClip.w <= 0.0001) {
                    t_prev = t_mid;
                    continue;
                }

                vec3 midNdc = midClip.xyz / midClip.w;
                vec2 midUv = midNdc.xy * 0.5 + 0.5;
                float midZRay = midNdc.z * 0.5 + 0.5;
                float midZTex = texture(uDepthTex, midUv).r;
                if (midZRay <= midZTex && midZTex > 0.0001) {
                    t_curr = t_mid;
                    hitZ = midZRay;
                    hitLocalPos = midLocalP;
                } else {
                    t_prev = t_mid;
                }
            }

            vec3 finalWorldP = hitLocalPos * uScale + uOffset;
            vec4 finalClipP = uVPInit * vec4(finalWorldP, 1.0);

            // Guard against points behind the FBO camera plane
            if (finalClipP.w <= 0.0001) {
                discard;
            }

            vec2 finalUv = (finalClipP.xy / finalClipP.w) * 0.5 + 0.5;

            vec4 currentClipP = uVPCurrent * vec4(finalWorldP, 1.0);
            hitZ = (currentClipP.z / currentClipP.w) * 0.5 + 0.5;
            hitW = currentClipP.w;

            finalColor = texture(uColorTex, finalUv);
            break;
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
