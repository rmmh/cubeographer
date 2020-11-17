const CopyPlugin = require("copy-webpack-plugin");
const path = require('path');

module.exports = {
  entry: './src/index.ts',
  module: {
    rules: [
      {
        test: /\.tsx?$/,
        use: 'ts-loader',
        exclude: /node_modules/,
      },
      {
          test: /\.glsl$/,
          loader: 'webpack-glsl-loader'
      }
    ],
  },
  resolve: {
    extensions: [ '.tsx', '.ts', '.js' ],
  },
  output: {
    filename: 'index.js',
    path: path.resolve(__dirname, 'dist'),
  },
  watch: true,
  plugins: [
    new CopyPlugin({
      patterns: [
        { from: "./src/index.html" },
        {
          from: "./node_modules/three/examples/fonts/helvetiker_regular.typeface.json",
          to: 'fonts/'
        },
      ],
    }),
  ],
};
