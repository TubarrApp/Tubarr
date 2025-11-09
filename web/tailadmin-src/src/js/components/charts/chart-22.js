import ApexCharts from "apexcharts";

// ===== chartOne
const chart22 = () => {
  const growthOptions = {
    series: [
      {
        name: "User Growth",
        data: [3500, 3580, 3620, 3550, 3650, 3700, 3768],
      },
    ],
    chart: {
      type: "area",
      height: 60,
      sparkline: {
        enabled: true,
      },
      animations: {
        enabled: true,
        easing: "easeinout",
        speed: 800,
      },
      toolbar: {
        show: false,
      },
    },
    colors: ["#10b981"],
    stroke: {
      curve: "smooth",
      width: 2,
    },
    fill: {
      type: "gradient",
      gradient: {
        shadeIntensity: 1,
        opacityFrom: 0.6,
        opacityTo: 0.1,
        stops: [0, 100],
      },
    },
    tooltip: {
      fixed: {
        enabled: false,
      },
      x: {
        show: false,
      },
      y: {
        formatter: function (value) {
          return value.toLocaleString();
        },
      },
      marker: {
        show: false,
      },
    },
  };

  const chartSelector = document.querySelectorAll("#chartTwentyTwo");

  if (chartSelector.length) {
    const chartTwentyTwo = new ApexCharts(
      document.querySelector("#chartTwentyTwo"),
      growthOptions,
    );
    chartTwentyTwo.render();
  }
};

export default chart22;
