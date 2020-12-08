export function generateMipmaps(gl: WebGL2RenderingContext, image: HTMLImageElement, levels: number) {
    if (image.width != image.height) {
        throw new Error("image isn't square");
    }
    if ((image.width&(image.width-1)) !== 0) {
        throw new Error("image isn't power of 2");
    }

    let lastDim = image.width;

    let lastCanvas = document.createElement('canvas');
    lastCanvas.width = image.width;
    lastCanvas.height = image.height;
    lastCanvas.getContext('2d').drawImage(image, 0, 0, image.width, image.height);
    let lastImage = lastCanvas.getContext('2d').getImageData(0, 0, lastCanvas.width, lastCanvas.height);

    // TODO: gamma-aware rescaling using sRGB<->linear transformation
    for (let level = 1; level <= levels; level++) {
        let dim = lastDim >> 1;
        let image = new ImageData(dim, dim);
        let ls = dim*2*4;
        const d = image.data;
        const ld = lastImage.data;
        for (let y = 0; y < dim; y++) {
            for (let x = 0; x < dim; x++) {
                let o = (x + y * dim) * 4;
                let lo = (x*2 + y*dim*4) * 4;
                d[o] = (ld[lo]+ld[lo+4]+ld[lo+ls]+ld[lo+ls+4])>>2;
                d[o+1] = (ld[lo+1]+ld[lo+5]+ld[lo+ls+1]+ld[lo+ls+5])>>2;
                d[o+2] = (ld[lo+2]+ld[lo+6]+ld[lo+ls+2]+ld[lo+ls+6])>>2;
                d[o+3] = Math.max(ld[lo+3], ld[lo+7], ld[lo+ls+3], ld[lo+ls+7]);
                /*
                d[o] = ld[lo];
                d[o+1] = ld[lo+1];
                d[o+2] = ld[lo+2];
                d[o+3] = ld[lo+3];*/
            }
        }
        gl.texImage2D(gl.TEXTURE_2D, level, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, image);
        lastDim = dim;
        lastImage = image;
    }
}