import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { openKioskFullscreen } from './kioskMode';

const mockGoto = vi.fn();

vi.mock('$app/navigation', () => ({
	goto: (...args: unknown[]) => mockGoto(...args)
}));

const mockRequestFullscreen = vi.fn();

describe('openKioskFullscreen', () => {
	beforeEach(() => {
		mockRequestFullscreen.mockResolvedValue(undefined);
		vi.stubGlobal('document', { documentElement: { requestFullscreen: mockRequestFullscreen } });
	});

	afterEach(() => {
		vi.clearAllMocks();
		vi.unstubAllGlobals();
	});

	it('goes fullscreen and navigates to the kiosk', async () => {
		await openKioskFullscreen();

		expect(mockRequestFullscreen).toHaveBeenCalledOnce();
		expect(mockGoto).toHaveBeenCalledWith('/kiosk');
	});

	// Fullscreen can be refused; the kiosk is still worth opening.
	it('still navigates when fullscreen is rejected', async () => {
		mockRequestFullscreen.mockRejectedValue(new Error('refused'));

		await expect(openKioskFullscreen()).resolves.toBeUndefined();
		expect(mockGoto).toHaveBeenCalledWith('/kiosk');
	});

	it('requests fullscreen before navigating, while the click still counts', async () => {
		const order: string[] = [];
		mockRequestFullscreen.mockImplementation(() => {
			order.push('fullscreen');
			return Promise.resolve();
		});
		mockGoto.mockImplementation(() => {
			order.push('goto');
			return Promise.resolve();
		});

		await openKioskFullscreen();

		expect(order).toEqual(['fullscreen', 'goto']);
	});
});
