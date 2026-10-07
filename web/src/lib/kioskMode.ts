import { goto } from '$app/navigation';
import { resolve } from '$app/paths';

/**
 * Dev-only shortcut into the kiosk view: the Pi's browser loads /kiosk fullscreen
 * on the panel, so a dev browser does the same to check the real framing, sizing
 * and overlay against the live picture.
 *
 * Fullscreen is requested before navigating, because it needs the click's user
 * activation; a SvelteKit client-side navigation keeps the same document, so the
 * fullscreen window survives the route swap.
 */
export async function openKioskFullscreen(): Promise<void> {
	try {
		await document.documentElement.requestFullscreen();
	} catch {
		// Fullscreen can be refused (embedded frame, permissions policy, an already
		// fullscreen document); the kiosk still opens, just windowed.
	}
	await goto(resolve('/kiosk'));
}
