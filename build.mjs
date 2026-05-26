import * as esbuild from 'esbuild';
import fs from 'node:fs/promises';

const isProd = process.argv.includes('--prod');

const glslPlugin = {
    name: 'glsl',
    setup(build) {
        build.onLoad({ filter: /\.glsl$/ }, async (args) => {
            const source = await fs.readFile(args.path, 'utf8');
            return {
                contents: source,
                loader: 'text',
            };
        });
    },
};

function formatBytes(bytes, dm = 2) {
    if (bytes == 0) return '0 B';
    var k = 1024,
        sizes = ['B', 'KiB', 'MiB', 'GiB'],
        i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + ' ' + sizes[i];
}


const cubeographerPlugin = {
    name: 'cubeographer build',
    setup(build) {
        build.onLoad({ filter: /src\/index\.ts$/ }, async (args) => {
            const source = await fs.readFile(args.path, 'utf8');
            return {
                contents: source,
                loader: 'ts',
                watchFiles: [
                    './src/index.html',
                    './src/map.html',
                    './src/index.css'
                ]
            };
        });

        build.onEnd(async result => {
            await fs.cp('./src/index.html', './dist/index.html');
            await fs.cp('./src/map.html', './dist/map.html')

            // Copy assets to go/dist/ for Go embedding
            await fs.mkdir('./go/dist', { recursive: true });
            await fs.cp('./dist/index.html', './go/dist/index.html');
            await fs.cp('./dist/map.html', './go/dist/map.html')
            await fs.cp('./dist/index.js', './go/dist/index.js');
            await fs.cp('./dist/index.css', './go/dist/index.css');

            const timestamp = new Date().toLocaleTimeString();
            if (result.errors.length > 0) {
                console.error(`❌ ${timestamp} Build failed with ${result.errors.length} error${result.errors.length != 1 ? "s" : ""}.`);
            } else {
                const totalSize = Object.values(result.metafile.outputs).reduce((acc, curr) => acc + curr.bytes, 0);
                console.log(`✅ ${timestamp} wrote ${formatBytes(totalSize)}`);
            }
        })
    },
}


const ctx = await esbuild.context({
    entryPoints: ['./src/index.ts'],
    bundle: true,
    metafile: true,
    outdir: 'dist',
    define: { DEBUG: (!isProd).toString() },
    sourcemap: 'linked',
    plugins: [cubeographerPlugin, glslPlugin],
});

await ctx.watch();
if (isProd) {
    await ctx.dispose();
} else {
    let { hosts, port } = await ctx.serve({
        servedir: 'dist',
    })
    console.log(`Serving on http://${hosts[1] || hosts[0]}:${port}`);
}
