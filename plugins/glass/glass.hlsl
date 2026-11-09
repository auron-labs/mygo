// The glass's effect in HLSL (see scene.Effect), as glass.metal is in
// Metal Shading Language and pixels.go draws it on the CPU.

float glassLens(float t, float bezel) {
	if (bezel <= 0 || t >= bezel) {
		return 0;
	}
	float u = 1 - max(t, 0) / bezel;
	float v = 1 - u * u * u * u;
	float a = sqrt(v * sqrt(v));
	float b = u * u * u;
	float n = sqrt(a * a + b * b);
	float s = b / n;
	float c = a / n;
	float st = s / 1.5;
	float ct = sqrt(1 - st * st);
	return (s * ct - c * st) / (c * ct + s * st) / 1.1180340;
}

float2 glassNormal(float2 p, float4 rect, float4 radii) {
	float2 h = rect.zw * 0.5;
	float2 q = p - rect.xy - h;
	float r = q.x < 0 ? (q.y < 0 ? radii.x : radii.w) : (q.y < 0 ? radii.y : radii.z);
	r = min(r * 1.5, min(h.x, h.y));
	float2 a = abs(q) - (h - r);
	float2 sq = float2(sign(q));
	if (a.x > 0 || a.y > 0) {
		float2 m = max(a, 0);
		return sq * m / sqrt(m.x * m.x + m.y * m.y);
	}
	return a.x > a.y ? float2(sq.x, 0) : float2(0, sq.y);
}

float3 glassTone(float3 c, float4 tone) {
	float l = 0.2126 * c.r + 0.7152 * c.g + 0.0722 * c.b;
	float t = tone.x + (tone.y - tone.x) * (1 - pow(max(1 - l, 0), tone.z));
	float k = (tone.y - tone.x) * tone.w;
	return saturate(t + (c - l) * k);
}

// glassEdge is glass.metal's.
float glassEdge(float l, float facing, float4 p3) {
	float k = p3.w + (p3.z - p3.w) * (1 - facing);
	return min(k / max(l, 1e-4), 0.67);
}

float4 effect(float2 p, Effect e) {
	float inside = -sdRoundRect(p, e.rect, e.radii) - 1;
	float t = max(inside, 0);
	float bezel = e.p0.x;
	float rimWidth = e.p0.w;
	float2 n = float2(0, 0);
	if (t < bezel || t < 3 * rimWidth || inside < 0.5) {
		n = glassNormal(p, e.rect, e.radii);
	}
	float facing = abs(n.x * e.p3.x + n.y * e.p3.y);
	float2 q = p;
	if (t < bezel) {
		q -= n * (e.p0.y * glassLens(t, bezel));
	}
	float3 c = glassTone(sampleBackdrop(e, q), e.p2);
	c += (e.p1.rgb - c) * e.p1.a;
	if (e.p0.z > 0 && t < 3 * rimWidth) {
		float r = t / rimWidth;
		c = min(c + facing * e.p0.z * exp(-r * r), 1);
	}
	float g = clamp(inside + 0.5, 0, 1);
	if (g >= 1) {
		return float4(c, 1);
	}
	float3 b = sampleBackdrop(e, p);
	float a = glassEdge(0.2126 * b.r + 0.7152 * b.g + 0.0722 * b.b, facing, e.p3);
	return float4(c * g, g + a * (1 - g));
}
