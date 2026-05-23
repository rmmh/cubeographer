export class HttpError extends Error {
    status: number;
    constructor(response: Response) {
        super(`HTTP error! status: ${response.status}`);
        this.name = 'HttpError';
        this.status = response.status;
    }
}