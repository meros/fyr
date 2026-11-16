# Fyr logo: an isometric lighthouse, rendered with Cycles on a transparent background.
#   blender -b -P lighthouse.py -- OUT_DIR [SIZE] [SAMPLES]
# Writes OUT_DIR/lighthouse-SIZE.png and OUT_DIR/lighthouse.blend.
import math
import sys

import bpy
from mathutils import Vector

argv = sys.argv[sys.argv.index("--") + 1:] if "--" in sys.argv else []
OUT = argv[0] if argv else "/tmp"
SIZE = int(argv[1]) if len(argv) > 1 else 512
SAMPLES = int(argv[2]) if len(argv) > 2 else 128

# Palette (design/system tokens): night, slate, tower white, amber light.
NIGHT = (0.078, 0.129, 0.176)
SLATE = (0.133, 0.216, 0.294)
WHITE = (0.93, 0.945, 0.957)
ROCK = (0.23, 0.28, 0.33)
ROCK_TOP = (0.40, 0.46, 0.50)
AMBER = (0.91, 0.588, 0.059)
LAMP = (1.0, 0.42, 0.04)  # deeper than the token: AgX lifts a bright emitter toward yellow


def srgb(c):
    return tuple(x / 12.92 if x <= 0.04045 else ((x + 0.055) / 1.055) ** 2.4 for x in c)


bpy.ops.wm.read_factory_settings(use_empty=True)
scene = bpy.context.scene


def material(name, color, rough=0.5, metal=0.0, emit=None, strength=0.0, alpha=1.0, transmission=0.0):
    m = bpy.data.materials.new(name)
    m.use_nodes = True
    b = m.node_tree.nodes["Principled BSDF"]
    b.inputs["Base Color"].default_value = (*srgb(color), 1)
    b.inputs["Roughness"].default_value = rough
    b.inputs["Metallic"].default_value = metal
    if emit:
        b.inputs["Emission Color"].default_value = (*srgb(emit), 1)
        b.inputs["Emission Strength"].default_value = strength
    if transmission:
        b.inputs["Transmission Weight"].default_value = transmission
        b.inputs["IOR"].default_value = 1.45
    if alpha < 1:
        b.inputs["Alpha"].default_value = alpha
    return m


def smooth(obj, bevel=0.0, segs=3):
    if bevel:
        mod = obj.modifiers.new("bevel", "BEVEL")
        mod.width, mod.segments, mod.limit_method = bevel, segs, "ANGLE"
    for p in obj.data.polygons:
        p.use_smooth = True
    return obj


def cyl(name, r1, r2, h, z, mat, verts=64, bevel=0.0):
    bpy.ops.mesh.primitive_cone_add(vertices=verts, radius1=r1, radius2=r2, depth=h, location=(0, 0, z + h / 2))
    o = bpy.context.object
    o.name = name
    o.data.materials.append(mat)
    return smooth(o, bevel)


m_white = material("tower", WHITE, rough=0.45)
m_band = material("band", SLATE, rough=0.5)
m_night = material("night", NIGHT, rough=0.35, metal=0.3)
m_rock = material("rock", ROCK, rough=0.85)
m_top = material("rocktop", ROCK_TOP, rough=0.9)
m_glass = material("glass", (0.85, 0.9, 0.95), rough=0.05, transmission=1.0)
m_lamp = material("lamp", LAMP, emit=LAMP, strength=14.0)
m_door = material("door", SLATE, rough=0.4)

# Rock base: a low rounded islet with a lighter top.
# A rounded mound with a few boulders around its edge.
bpy.ops.mesh.primitive_uv_sphere_add(radius=1.0, segments=48, ring_count=24, location=(0, 0, -0.05))
mound = bpy.context.object
mound.scale = (1.05, 1.05, 0.3)
mound.data.materials.append(m_rock)
smooth(mound)
for a, r, s_ in ((20, 0.92, 0.30), (75, 0.98, 0.23), (140, 0.88, 0.27), (200, 0.96, 0.21), (250, 0.9, 0.28), (310, 0.98, 0.24), (345, 0.84, 0.2)):
    t = math.radians(a)
    bpy.ops.mesh.primitive_ico_sphere_add(subdivisions=1, radius=s_, location=(r * math.cos(t), r * math.sin(t), -0.02))
    b = bpy.context.object
    b.scale = (1.0, 0.85, 0.7)
    b.rotation_euler = (0.3, 0.2, t)
    b.data.materials.append(m_top if a % 2 else m_rock)
    mod = b.modifiers.new("bevel", "BEVEL")
    mod.width, mod.segments = 0.02, 2

# Tower: tapered, two slate bands.
H = 2.5
R0, R1 = 0.62, 0.42
cyl("tower", R0, R1, H, 0.06, m_white, bevel=0.02)
cyl("plinth", R0 + 0.08, R0 + 0.06, 0.14, 0.0, m_band, bevel=0.02)


def r_at(z):
    return R0 + (R1 - R0) * (z - 0.06) / H


for z0, z1 in ((0.75, 1.05), (1.65, 1.95)):
    cyl("band", r_at(z0) + 0.006, r_at(z1) + 0.006, z1 - z0, z0, m_band)

# Door, facing the camera's side (+x +y).
bpy.ops.mesh.primitive_cube_add(size=1, location=(0, 0, 0.36))
d = bpy.context.object
d.scale = (0.08, 0.2, 0.3)
d.location = Vector((1, 1, 0)).normalized() * (R0 - 0.03) + Vector((0, 0, 0.32))
d.rotation_euler = (0, 0, math.radians(45))
d.data.materials.append(m_door)
smooth(d, 0.02)

top = 0.06 + H
# Gallery deck and railing.
cyl("gallery", 0.66, 0.66, 0.09, top, m_night, bevel=0.02)
bpy.ops.mesh.primitive_torus_add(major_radius=0.62, minor_radius=0.018, location=(0, 0, top + 0.27))
rail = bpy.context.object
rail.data.materials.append(m_night)
smooth(rail)
for i in range(16):
    a = i / 16 * 2 * math.pi
    bpy.ops.mesh.primitive_cylinder_add(radius=0.014, depth=0.2, location=(0.62 * math.cos(a), 0.62 * math.sin(a), top + 0.18))
    bpy.context.object.data.materials.append(m_night)

# Lantern room: glass with the amber lamp inside, thin frame posts.
lr = 0.34
cyl("lantern", lr, lr, 0.46, top + 0.09, m_glass, verts=48)
bpy.ops.mesh.primitive_uv_sphere_add(radius=0.17, location=(0, 0, top + 0.32))
lamp = bpy.context.object
lamp.data.materials.append(m_lamp)
smooth(lamp)
for i in range(6):
    a = i / 6 * 2 * math.pi + math.pi / 6
    bpy.ops.mesh.primitive_cylinder_add(radius=0.022, depth=0.46, location=(lr * math.cos(a), lr * math.sin(a), top + 0.32))
    bpy.context.object.data.materials.append(m_night)

# Cap: a dome with a finial.
cap_z = top + 0.55
cyl("caprim", 0.42, 0.42, 0.05, cap_z, m_night, bevel=0.015)
bpy.ops.mesh.primitive_uv_sphere_add(radius=0.38, location=(0, 0, cap_z + 0.05))
dome = bpy.context.object
dome.scale = (1, 1, 0.75)
dome.data.materials.append(m_night)
smooth(dome)
bpy.ops.mesh.primitive_uv_sphere_add(radius=0.06, location=(0, 0, cap_z + 0.38))
bpy.context.object.data.materials.append(m_night)
smooth(bpy.context.object)
# Cut the dome's lower half away by hiding it under the rim: a boolean keeps it clean.
bpy.ops.mesh.primitive_cube_add(size=2, location=(0, 0, cap_z + 0.05 - 1))
cutter = bpy.context.object
mod = dome.modifiers.new("cut", "BOOLEAN")
mod.object, mod.operation = cutter, "DIFFERENCE"
cutter.hide_render = True
cutter.hide_viewport = True

# The lamp lights its surroundings.
bpy.ops.object.light_add(type="POINT", location=(0, 0, top + 0.32))
pl = bpy.context.object.data
pl.energy, pl.color, pl.shadow_soft_size = 120, srgb(LAMP), 0.15

# Key light from upper left of the view, soft fill from the right, a world for ambient.
bpy.ops.object.light_add(type="SUN", rotation=(math.radians(50), 0, math.radians(-20)))
sun = bpy.context.object.data
sun.energy, sun.angle = 4.6, math.radians(8)
bpy.ops.object.light_add(type="AREA", location=(4, -2, 5))
fill = bpy.context.object
fill.data.energy, fill.data.size = 250, 4
fill.rotation_euler = (Vector((0, 0, 1.5)) - fill.location).to_track_quat("-Z", "Y").to_euler()
world = bpy.data.worlds.new("w")
scene.world = world
world.use_nodes = True
world.node_tree.nodes["Background"].inputs["Color"].default_value = (*srgb((0.62, 0.68, 0.75)), 1)
world.node_tree.nodes["Background"].inputs["Strength"].default_value = 0.75

# Rim light from behind the view: it outlines the dark cap on a dark page.
bpy.ops.object.light_add(type="AREA", location=(-4, -4, 4.5))
rim = bpy.context.object
rim.data.energy, rim.data.size, rim.data.color = 420, 3, srgb((0.78, 0.86, 1.0))
rim.rotation_euler = (Vector((0, 0, 2.2)) - rim.location).to_track_quat("-Z", "Y").to_euler()

# True isometric camera: orthographic, 35.264° down, 45° around.
bpy.ops.object.camera_add()
cam = bpy.context.object
scene.camera = cam
cam.data.type = "ORTHO"
cam.rotation_euler = (math.radians(90 - 35.264), 0, math.radians(135))
# Frame every visible mesh with an even margin: project its corners into the camera's plane.
bpy.context.view_layer.update()
rot = cam.rotation_euler.to_matrix()
inv = rot.inverted()
def frame():
    bpy.context.view_layer.update()
    dg = bpy.context.evaluated_depsgraph_get()
    xs, ys = [], []
    for o in scene.objects:
        if o.type != "MESH" or o.hide_render:
            continue
        e = o.evaluated_get(dg)
        for v in e.to_mesh().vertices:
            w = inv @ (e.matrix_world @ v.co)
            xs.append(w.x)
            ys.append(w.y)
        e.to_mesh_clear()
    cx, cy = (min(xs) + max(xs)) / 2, (min(ys) + max(ys)) / 2
    cam.location = rot @ Vector((cx, cy, 20))
    cam.data.ortho_scale = max(max(xs) - min(xs), max(ys) - min(ys)) * 1.04
cam.data.clip_end = 100

# Two beams across the picture: amber, strong at the lamp, fading outward. They cast no
# shadow and hide nothing behind them.
side = rot @ Vector((1, 0, 0))
side.z = 0
side.normalize()
beam_len = 1.7
for sign in (1, -1):
    bm = bpy.data.materials.new("beam")
    bm.use_nodes = True
    nt = bm.node_tree
    for n in list(nt.nodes):
        nt.nodes.remove(n)
    out = nt.nodes.new("ShaderNodeOutputMaterial")
    mix = nt.nodes.new("ShaderNodeMixShader")
    tr = nt.nodes.new("ShaderNodeBsdfTransparent")
    em = nt.nodes.new("ShaderNodeEmission")
    em.inputs["Color"].default_value = (*srgb(LAMP), 1)
    em.inputs["Strength"].default_value = 3.0
    tc = nt.nodes.new("ShaderNodeTexCoord")
    sep = nt.nodes.new("ShaderNodeSeparateXYZ")
    mr = nt.nodes.new("ShaderNodeMapRange")
    mr.inputs["From Min"].default_value = -beam_len / 2
    mr.inputs["From Max"].default_value = beam_len / 2
    mr.inputs["To Min"].default_value = 0.0
    mr.inputs["To Max"].default_value = 0.92
    pw = nt.nodes.new("ShaderNodeMath")
    pw.operation = "POWER"
    pw.inputs[1].default_value = 1.8
    nt.links.new(tc.outputs["Object"], sep.inputs[0])
    nt.links.new(sep.outputs["Z"], mr.inputs["Value"])
    nt.links.new(mr.outputs["Result"], pw.inputs[0])
    nt.links.new(pw.outputs[0], mix.inputs["Fac"])
    nt.links.new(tr.outputs[0], mix.inputs[1])
    nt.links.new(em.outputs[0], mix.inputs[2])
    nt.links.new(mix.outputs[0], out.inputs["Surface"])
    bm.blend_method = "BLEND" if hasattr(bm, "blend_method") else None
    lamp_c = Vector((0, 0, top + 0.32))
    centre_b = lamp_c + sign * side * (beam_len / 2 + lr + 0.04)  # starts outside the glass
    # radius1 at local -z (far end, wide), radius2 at +z (the lamp, narrow); +z points at the lamp.
    bpy.ops.mesh.primitive_cone_add(vertices=48, radius1=0.40, radius2=0.15, depth=beam_len, location=centre_b)
    beam = bpy.context.object
    beam.rotation_euler = (-sign * side).to_track_quat("Z", "Y").to_euler()
    beam.data.materials.append(bm)
    beam.visible_shadow = False
    smooth(beam)

frame()
r = scene.render
r.engine = "CYCLES"
scene.cycles.samples = SAMPLES
scene.cycles.use_denoising = True
scene.cycles.device = "CPU"
r.film_transparent = True
r.resolution_x = r.resolution_y = SIZE
r.resolution_percentage = 100
r.image_settings.file_format = "PNG"
r.image_settings.color_mode = "RGBA"
scene.view_settings.view_transform = "AgX" if "AgX" in [v.identifier for v in type(scene.view_settings).bl_rna.properties["view_transform"].enum_items] else "Filmic"
scene.view_settings.look = "None"
r.filepath = f"{OUT}/lighthouse-{SIZE}.png"
bpy.ops.render.render(write_still=True)
# The scene itself, for editing in Blender by hand.
bpy.ops.wm.save_as_mainfile(filepath=f"{OUT}/lighthouse.blend", compress=True)
print("WROTE", r.filepath)
