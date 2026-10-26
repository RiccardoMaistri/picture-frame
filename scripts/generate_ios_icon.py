import os
import math
from PIL import Image, ImageDraw, ImageFilter, ImageOps

def find_perspective_coeffs(src_coords, dst_coords):
    # src_coords and dst_coords are lists of 4 (x,y) tuples
    matrix = []
    for (x, y), (u, v) in zip(dst_coords, src_coords):
        matrix.append([x, y, 1, 0, 0, 0, -u*x, -u*y])
        matrix.append([0, 0, 0, x, y, 1, -v*x, -v*y])

    A = matrix
    B = []
    for (x, y), (u, v) in zip(dst_coords, src_coords):
        B.append(u)
        B.append(v)

    # Solve A * X = B using Gaussian elimination
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

def create_ios_icon():
    canvas_size = 1024
    img = Image.new("RGBA", (canvas_size, canvas_size), (0, 0, 0, 255))

    # Create background with soft dark gradient and subtle central spotlight
    bg = Image.new("RGBA", (canvas_size, canvas_size), (15, 20, 30, 255))
    bg_draw = ImageDraw.Draw(bg)

    # Radial glow in center
    cx, cy = canvas_size // 2, canvas_size // 2
    glow = Image.new("RGBA", (canvas_size, canvas_size), (0, 0, 0, 0))
    glow_draw = ImageDraw.Draw(glow)
    for r in range(450, 0, -5):
        alpha = int(35 * (1 - r / 450))
        glow_draw.ellipse([cx - r, cy - r, cx + r, cy + r], fill=(50, 80, 130, alpha))
    bg = Image.alpha_composite(bg, glow)

    # Surface Tablet Perspective Coordinates (1024x1024 space)
    outer_quad = [
        (180, 240),  # TL
        (840, 180),  # TR
        (800, 780),  # BR
        (220, 840)   # BL
    ]

    screen_quad = [
        (210, 270),  # TL
        (810, 215),  # TR
        (775, 745),  # BR
        (245, 798)   # BL
    ]

    # Shadow layer
    shadow = Image.new("RGBA", (canvas_size, canvas_size), (0, 0, 0, 0))
    shadow_draw = ImageDraw.Draw(shadow)

    shadow_quad = [(x + 20, y + 45) for x, y in outer_quad]
    shadow_draw.polygon(shadow_quad, fill=(0, 0, 0, 160))
    shadow = shadow.filter(ImageFilter.GaussianBlur(30))

    bg = Image.alpha_composite(bg, shadow)

    # Outer Surface Body / Kickstand bevel (Dark Magnesium / Titanium finish)
    body = Image.new("RGBA", (canvas_size, canvas_size), (0, 0, 0, 0))
    body_draw = ImageDraw.Draw(body)

    # Outer frame
    body_draw.polygon(outer_quad, fill=(32, 35, 42, 255), outline=(65, 70, 82, 255))

    # Metallic edge highlight on top & left
    body_draw.line([outer_quad[3], outer_quad[0], outer_quad[1]], fill=(140, 150, 170, 220), width=6)

    bg = Image.alpha_composite(bg, body)

    # Render Content onto the Screen
    sw, sh = 1200, 800
    screen_src = Image.new("RGBA", (sw, sh), (12, 14, 20, 255))

    # Subtle dark wallpaper pattern/gradient on the Surface screen
    screen_draw = ImageDraw.Draw(screen_src)
    for y in range(sh):
        interp = y / sh
        r = int(18 * (1 - interp) + 8 * interp)
        g = int(24 * (1 - interp) + 12 * interp)
        b = int(36 * (1 - interp) + 20 * interp)
        screen_draw.line([(0, y), (sw, y)], fill=(r, g, b, 255))

    # Load Immich Logo & Remove white box background
    immich_path = "/tmp/file_attachments/image.png"
    if os.path.exists(immich_path):
        immich_img = Image.open(immich_path).convert("RGBA")

        # Transparent background for white/near-white pixels
        datas = immich_img.getdata()
        newData = []
        for item in datas:
            if item[0] > 240 and item[1] > 240 and item[2] > 240:
                newData.append((255, 255, 255, 0))
            else:
                newData.append(item)
        immich_img.putdata(newData)

        # Resize Immich logo to fit nicely on the Surface screen
        logo_size = 480
        immich_img = immich_img.resize((logo_size, logo_size), Image.Resampling.LANCZOS)

        # Center logo on Surface screen
        lx = (sw - logo_size) // 2
        ly = (sh - logo_size) // 2
        screen_src.paste(immich_img, (lx, ly), immich_img)

    # Add subtle glass glare line on screen before perspective transformation
    glare = Image.new("RGBA", (sw, sh), (0, 0, 0, 0))
    glare_draw = ImageDraw.Draw(glare)
    glare_draw.polygon([(0, 0), (sw * 0.7, 0), (0, sh * 0.7)], fill=(255, 255, 255, 22))
    screen_src = Image.alpha_composite(screen_src, glare)

    # Warp screen_src into screen_quad
    src_corners = [(0, 0), (sw, 0), (sw, sh), (0, sh)]
    coeffs = find_perspective_coeffs(src_corners, screen_quad)

    warped_screen = screen_src.transform(
        (canvas_size, canvas_size),
        Image.Transform.PERSPECTIVE,
        coeffs,
        Image.Resampling.BICUBIC
    )

    bg = Image.alpha_composite(bg, warped_screen)

    # Screen Glass Bezel Border Highlight
    bezel_draw = ImageDraw.Draw(bg)
    bezel_draw.polygon(screen_quad, outline=(50, 55, 65, 255), width=3)

    # Save outputs
    os.makedirs("web/static", exist_ok=True)

    # 512x512
    icon512 = bg.resize((512, 512), Image.Resampling.LANCZOS)
    icon512.save("web/static/icon-512.png", "PNG")

    # 180x180 (Apple Touch Icon)
    icon180 = bg.resize((180, 180), Image.Resampling.LANCZOS)
    icon180.save("web/static/apple-touch-icon.png", "PNG")

    # 192x192 (PWA Icon)
    icon192 = bg.resize((192, 192), Image.Resampling.LANCZOS)
    icon192.save("web/static/icon-192.png", "PNG")

    print("Generated apple-touch-icon.png (180x180), icon-512.png, icon-192.png successfully.")

if __name__ == "__main__":
    create_ios_icon()
