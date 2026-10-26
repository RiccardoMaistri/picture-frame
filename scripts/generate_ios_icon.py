import os
import math
from PIL import Image, ImageDraw, ImageFilter

def find_perspective_coeffs(src_coords, dst_coords):
    matrix = []
    for (x, y), (u, v) in zip(dst_coords, src_coords):
        matrix.append([x, y, 1, 0, 0, 0, -u*x, -u*y])
        matrix.append([0, 0, 0, x, y, 1, -v*x, -v*y])

    A = matrix
    B = []
    for (x, y), (u, v) in zip(dst_coords, src_coords):
        B.append(u)
        B.append(v)

    n = 8
    for i in range(n):
        max_row = i
        for k in range(i+1, n):
            if abs(A[k][i]) > abs(A[max_row][i]):
                max_row = k
        A[i], A[max_row] = A[max_row], A[i]
        B[i], B[max_row] = B[max_row], B[i]

        pivot = A[i][i]
        for j in range(i, n):
            A[i][j] /= pivot
        B[i] /= pivot

        for k in range(n):
            if k != i:
                factor = A[k][i]
                for j in range(i, n):
                    A[k][j] -= factor * A[i][j]
                B[k] -= factor * B[i]

    return B

def draw_immich_logo(size=800):
    logo = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(logo)

    cx, cy = size / 2, size / 2
    r_outer = size * 0.42
    r_inner = size * 0.08

    colors = [
        (239, 68, 68, 255),   # Red
        (245, 158, 11, 255),  # Amber
        (16, 185, 129, 255),  # Green
        (59, 130, 246, 255),  # Blue
        (236, 72, 153, 255)   # Pink
    ]

    for i in range(5):
        angle = i * 72.0 - 90.0
        color = colors[i]

        points = []
        num_steps = 100
        for step in range(num_steps + 1):
            t = step / num_steps
            rad = t * math.pi
            w = math.sin(rad) ** 0.8 * (r_outer - r_inner)
            d = r_inner + t * (r_outer - r_inner)
            curve = math.sin(t * math.pi) * 0.25 * (r_outer - r_inner)

            px = d
            py = curve + w * 0.4

            a_rad = math.radians(angle)
            rx = cx + px * math.cos(a_rad) - py * math.sin(a_rad)
            ry = cy + px * math.sin(a_rad) + py * math.cos(a_rad)
            points.append((rx, ry))

        for step in range(num_steps, -1, -1):
            t = step / num_steps
            rad = t * math.pi
            w = math.sin(rad) ** 0.8 * (r_outer - r_inner)
            d = r_inner + t * (r_outer - r_inner)
            curve = math.sin(t * math.pi) * 0.25 * (r_outer - r_inner)

            px = d
            py = curve - w * 0.4

            a_rad = math.radians(angle)
            rx = cx + px * math.cos(a_rad) - py * math.sin(a_rad)
            ry = cy + px * math.sin(a_rad) + py * math.cos(a_rad)
            points.append((rx, ry))

        draw.polygon(points, fill=color)

    return logo

def create_ios_icon():
    canvas_size = 1024

    # Background: Modern iOS dark background gradient
    bg = Image.new("RGBA", (canvas_size, canvas_size), (15, 23, 42, 255))

    # Central soft ambient glow
    cx, cy = canvas_size // 2, canvas_size // 2
    glow = Image.new("RGBA", (canvas_size, canvas_size), (0, 0, 0, 0))
    glow_draw = ImageDraw.Draw(glow)
    for r in range(480, 0, -5):
        alpha = int(45 * (1 - r / 480))
        glow_draw.ellipse([cx - r, cy - r, cx + r, cy + r], fill=(56, 189, 248, alpha))
    bg = Image.alpha_composite(bg, glow)

    # Load user-provided Surface icon (/tmp/file_attachments/image.png)
    surface_path = "/tmp/file_attachments/image.png"
    surface_raw = Image.open(surface_path).convert("RGBA")

    # Scale surface line-art image to canvas
    surface_scaled = surface_raw.resize((canvas_size, canvas_size), Image.Resampling.LANCZOS)

    # Recolor black lines into bright silver-white lines including the Surface pen
    flat = surface_scaled.get_flattened_data() if hasattr(surface_scaled, 'get_flattened_data') else surface_scaled.getdata()
    lines_data = [(240, 245, 255, pixel[3]) for pixel in flat]

    surface_lines = Image.new("RGBA", (canvas_size, canvas_size))
    surface_lines.putdata(lines_data)

    # Screen inner polygon coordinates in 1024x1024 space:
    # TL: (182, 195), TR: (686, 195), BR: (878, 689), BL: (389, 865)
    screen_quad = [
        (182, 195),
        (686, 195),
        (878, 689),
        (389, 865)
    ]

    # Fill screen area with dark display glass background
    screen_mask = Image.new("L", (canvas_size, canvas_size), 0)
    screen_mask_draw = ImageDraw.Draw(screen_mask)
    screen_mask_draw.polygon(screen_quad, fill=255)

    screen_bg = Image.new("RGBA", (canvas_size, canvas_size), (10, 15, 26, 255))
    bg.paste(screen_bg, (0, 0), screen_mask)

    # Render Immich Logo onto screen_quad in perspective
    logo_size = 800
    immich_logo = draw_immich_logo(logo_size)

    src_corners = [(0, 0), (logo_size, 0), (logo_size, logo_size), (0, logo_size)]
    coeffs = find_perspective_coeffs(src_corners, screen_quad)

    warped_logo = immich_logo.transform(
        (canvas_size, canvas_size),
        Image.Transform.PERSPECTIVE,
        coeffs,
        Image.Resampling.BICUBIC
    )

    # Composite logo onto screen
    bg = Image.alpha_composite(bg, warped_logo)

    # Add subtle glass glare reflection across the screen
    glare = Image.new("RGBA", (canvas_size, canvas_size), (0, 0, 0, 0))
    glare_draw = ImageDraw.Draw(glare)
    glare_draw.polygon([(182, 195), (500, 195), (250, 700)], fill=(255, 255, 255, 18))
    bg = Image.alpha_composite(bg, glare)

    # Composite the white Surface tablet line art (kickstand, pen, bezels) on top
    bg = Image.alpha_composite(bg, surface_lines)

    # Save output images
    os.makedirs("web/static", exist_ok=True)

    icon512 = bg.resize((512, 512), Image.Resampling.LANCZOS)
    icon512.save("web/static/icon-512.png", "PNG")

    icon180 = bg.resize((180, 180), Image.Resampling.LANCZOS)
    icon180.save("web/static/apple-touch-icon.png", "PNG")

    icon192 = bg.resize((192, 192), Image.Resampling.LANCZOS)
    icon192.save("web/static/icon-192.png", "PNG")

    print("Generated apple-touch-icon.png (180x180), icon-512.png, icon-192.png successfully.")

if __name__ == "__main__":
    create_ios_icon()
