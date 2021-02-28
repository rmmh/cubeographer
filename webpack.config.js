const CopyPlugin = require("copy-webpack-plugin");
const path = require('path');

module.exports = {
  mode: 'production',
  entry: './src/index.ts',
  devtool: 'source-map',
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
      ],
    }),
  ],
};
