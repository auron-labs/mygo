// The glass's effect in GLSL (see scene.Effect), as glass.metal is in
// Metal Shading Language and pixels.go draws it on the CPU.

float glassLens(float t, float bezel) {
	if (bezel <= 0.0 || t >= bezel) {
		return 0.0;
	}
	float u = 1.0 - max(t, 0.0) / bezel;
	float v = 1.0 - u * u * u * u;
	float a = sqrt(v * sqrt(v));
	float b = u * u * u;
	float n = sqrt(a * a + b * b);
	float s = b / n;
	float c = a / n;
	float st = s / 1.5;
	float ct = sqrt(1.0 - st * st);
	return (s * ct - c * st) / (c * ct + s * st) / 1.1180340;
}

vec2 glassNormal(vec2 p, vec4 rect, vec4 radii) {
	vec2 h = rect.zw * 0.5;
	vec2 q = p - rect.xy - h;
	float r = q.x < 0.0 ? (q.y < 0.0 ? radii.x : radii.w) : (q.y < 0.0 ? radii.y : radii.z);
	r = min(r * 1.5, min(h.x, h.y));
	vec2 a = abs(q) - (h - r);
	if (a.x > 0.0 || a.y > 0.0) {
		vec2 m = max(a, vec2(0.0));
		return sign(q) * m / sqrt(m.x * m.x + m.y * m.y);
	}
	return a.x > a.y ? vec2(sign(q.x), 0.0) : vec2(0.0, sign(q.y));
}

vec3 glassTone(vec3 c, vec4 tone) {
	float l = 0.2126 * c.r + 0.7152 * c.g + 0.0722 * c.b;
	float t = tone.x + (tone.y - tone.x) * (1.0 - pow(max(1.0 - l, 0.0), tone.z));
	float k = (tone.y - tone.x) * tone.w;
	return clamp(t + (c - l) * k, 0.0, 1.0);
}

// glassEdge is glass.metal's.
float glassEdge(float l, float facing, vec4 p3) {
	float k = p3.w + (p3.z - p3.w) * (1.0 - facing);
	return min(k / max(l, 1e-4), 0.67);
}

vec4 effect(vec2 p, Effect e) {
	float inside = -sdRoundRect(p, e.rect, e.radii) - 1.0;
	float t = max(inside, 0.0);
	float bezel = e.p0.x;
	float rimWidth = e.p0.w;
	vec2 n = vec2(0.0);
	if (t < bezel || t < 3.0 * rimWidth || inside < 0.5) {
		n = glassNormal(p, e.rect, e.radii);
	}
	float facing = abs(n.x * e.p3.x + n.y * e.p3.y);
	vec2 q = p;
	if (t < bezel) {
		q -= n * (e.p0.y * glassLens(t, bezel));
	}
	vec3 c = glassTone(sampleBackdrop(e, q), e.p2);
	c += (e.p1.rgb - c) * e.p1.a;
	if (e.p0.z > 0.0 && t < 3.0 * rimWidth) {
		float r = t / rimWidth;
		c = min(c + facing * e.p0.z * exp(-r * r), vec3(1.0));
	}
	float g = clamp(inside + 0.5, 0.0, 1.0);
	if (g >= 1.0) {
		return vec4(c, 1.0);
	}
	vec3 b = sampleBackdrop(e, p);
	float a = glassEdge(0.2126 * b.r + 0.7152 * b.g + 0.0722 * b.b, facing, e.p3);
	return vec4(c * g, g + a * (1.0 - g));
}
